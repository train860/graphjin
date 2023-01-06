package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/dosco/graphjin/v2/core/internal/psql"
	"github.com/dosco/graphjin/v2/core/internal/qcode"
	"github.com/dosco/graphjin/v2/core/internal/valid"
	plugin "github.com/dosco/graphjin/v2/plugin"
)

type gstate struct {
	gj    *graphjin
	r     graphqlReq
	cs    *cstate
	data  []byte
	dhash [sha256.Size]byte
	role  string
}

type cstate struct {
	sync.Once
	st  stmt
	err error
}

type stmt struct {
	role string
	roc  *Role
	qc   *qcode.QCode
	md   psql.Metadata
	va   *valid.Validate
	sql  string
}

func newGState(gj *graphjin, r graphqlReq, role string) (s gstate) {
	s.gj = gj
	s.r = r
	s.role = role
	return
}

func (s *gstate) compile() (err error) {
	if !s.gj.prod {
		err = s.compileQueryForRole()
		return

	}

	// In production mode and compile and cache the result
	// In production mode the query is derived from the allow list
	err = s.compileQueryForRoleOnce()
	return
}

func (s *gstate) compileQueryForRoleOnce() (err error) {
	k := (s.r.ns + s.r.name + s.role)

	val, loaded := s.gj.queries.LoadOrStore(k, &cstate{})
	s.cs = val.(*cstate)
	err = s.cs.err

	if loaded {
		return
	}

	s.cs.Do(func() {
		err = s.compileQueryForRole()
		s.cs.err = err
	})
	return
}

func (s *gstate) compileQueryForRole() (err error) {
	st := stmt{role: s.role}

	var ok bool
	if st.roc, ok = s.gj.roles[s.role]; !ok {
		err = fmt.Errorf(`roles '%s' not defined in c.gj.config`, s.role)
		return
	}

	var vars json.RawMessage
	if len(s.r.aschema) != 0 {
		vars = s.r.aschema
	} else {
		vars = s.r.vars
	}

	if st.qc, err = s.gj.qc.Compile(
		s.r.query,
		vars,
		s.role,
		s.r.ns); err != nil {
		return
	}

	var w bytes.Buffer

	if st.md, err = s.gj.pc.Compile(&w, st.qc); err != nil {
		return
	}

	if st.qc.Validation.Source != "" {
		vc, ok := s.gj.validatorMap[st.qc.Validation.Type]
		if !ok {
			err = fmt.Errorf("no validator found for '%s'", st.qc.Validation.Type)
			return
		}

		var ve plugin.ValidationExecuter
		ve, err = vc.CompileValidation(st.qc.Validation.Source)
		if err != nil {
			return
		}
		st.qc.Validation.VE = ve
		st.qc.Validation.Exists = true
	}

	if st.qc.Script.Name != "" {
		if err = s.gj.loadScript(st.qc); err != nil {
			return
		}
	}

	st.va = valid.New()
	st.sql = w.String()

	if s.cs == nil {
		s.cs = &cstate{st: st}
	} else {
		// s.cs.r = s.r
		s.cs.st = st
	}

	return
}

func (s *gstate) compileAndExecuteWrapper(c context.Context) (err error) {
	if err = s.compileAndExecute(c); err != nil {
		return
	}

	if s.gj.conf.Debug {
		s.debugLogStmt()
	}

	if len(s.data) == 0 {
		return
	}

	cs := s.cs

	if cs.st.qc.Remotes != 0 {
		if err = s.execRemoteJoin(c); err != nil {
			return
		}
	}

	qc := cs.st.qc

	if qc.Script.Exists && qc.Script.HasRespFn() {
		err = s.scriptCallResp(c)
	}
	return
}

func (s *gstate) compileAndExecute(c context.Context) (err error) {
	var conn *sql.Conn

	// get a new database connection
	c1, span1 := s.gj.spanStart(c, "Get Connection")
	defer span1.End()

	err = retryOperation(c1, func() (err1 error) {
		conn, err1 = s.gj.db.Conn(c1)
		return
	})
	if err != nil {
		span1.Error(err)
		return
	}
	defer conn.Close()

	// set the local user id on the connection if needed
	if s.gj.conf.SetUserID {
		c1, span2 := s.gj.spanStart(c, "Set Local User ID")
		defer span2.End()

		err = retryOperation(c1, func() (err1 error) {
			return s.setLocalUserID(c1, conn)
		})
		if err != nil {
			span2.Error(err)
			return
		}
	}

	// get the role from context or using the role_query
	if v := c.Value(UserRoleKey); v != nil {
		s.role = v.(string)
	} else if s.gj.abacEnabled {
		err = s.executeRoleQuery(c, conn)
	}
	if err != nil {
		return
	}

	// compile query for the role
	if err = s.compile(); err != nil {
		return
	}
	err = s.execute(c, conn)
	return
}

func (s *gstate) execute(c context.Context, conn *sql.Conn) (err error) {
	if err = s.validateAndUpdateVars(c); err != nil {
		return
	}

	var args args
	if args, err = s.argList(c); err != nil {
		return
	}

	cs := s.cs

	c1, span := s.gj.spanStart(c, "Execute Query")
	defer span.End()

	err = retryOperation(c1, func() (err1 error) {
		return conn.
			QueryRowContext(c1, cs.st.sql, args.values...).
			Scan(&s.data)
	})

	if err != nil && err != sql.ErrNoRows {
		span.Error(err)
	}

	if span.IsRecording() {
		span.SetAttributesString(
			stringAttr{"query.namespace", s.r.ns},
			stringAttr{"query.operation", cs.st.qc.Type.String()},
			stringAttr{"query.name", cs.st.qc.Name},
			stringAttr{"query.role", cs.st.role})
	}

	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		return
	}

	s.dhash = sha256.Sum256(s.data)

	s.data, err = encryptValues(s.data,
		s.gj.pf, decPrefix, s.dhash[:], s.gj.encKey)

	return
}

func (s *gstate) executeRoleQuery(c context.Context, conn *sql.Conn) (err error) {
	s.role, err = s.gj.executeRoleQuery(c, conn, s.r.vars, s.r.rc)
	return
}

func (s *gstate) argList(c context.Context) (args args, err error) {
	args, err = s.gj.argList(c, s.cs.st.md, s.r.vars, s.r.rc)
	return
}

func (s *gstate) argListVars(c context.Context, vars json.RawMessage) (
	args args, err error) {
	args, err = s.gj.argList(c, s.cs.st.md, vars, s.r.rc)
	return
}

func (s *gstate) setLocalUserID(c context.Context, conn *sql.Conn) (err error) {
	if v := c.Value(UserIDKey); v == nil {
		return nil
	} else {
		switch v1 := v.(type) {
		case string:
			_, err = conn.ExecContext(c, `SET SESSION "user.id" = '`+v1+`'`)

		case int:
			_, err = conn.ExecContext(c, `SET SESSION "user.id" = `+strconv.Itoa(v1))
		}
	}
	return
}

func (s *gstate) validateAndUpdateVars(c context.Context) (err error) {
	var vars map[string]interface{}

	cs := s.cs
	qc := cs.st.qc

	if qc == nil {
		return nil
	}

	if qc.Consts != nil || (qc.Script.Exists && qc.Script.HasReqFn()) {
		vars = make(map[string]interface{})

		if len(s.r.vars) != 0 {
			if err := json.Unmarshal(s.r.vars, &vars); err != nil {
				return err
			}
		}
	}

	if qc.Validation.Exists {
		if err := qc.Validation.VE.Validate(s.r.vars); err != nil {
			return err
		}
	}

	if qc.Consts != nil {
		errs := cs.st.va.ValidateMap(c, vars, qc.Consts)
		if !s.gj.prod && len(errs) != 0 {
			for k, v := range errs {
				s.gj.log.Printf("validation failed: $%s: %s", k, v.Error())
			}
		}

		if len(errs) != 0 {
			return errors.New("validation failed")
		}
	}

	if qc.Script.Exists && qc.Script.HasReqFn() {
		var v []byte
		if v, err = s.scriptCallReq(c, qc, vars, s.role); err != nil {
			return
		}
		s.r.vars = v
	}
	return
}

func (s *gstate) sql() string {
	if s.cs != nil && s.cs.st.qc != nil {
		return s.cs.st.sql
	}
	return ""
}

func (s *gstate) cacheHeader() string {
	if s.cs != nil && s.cs.st.qc != nil {
		return s.cs.st.qc.Cache.Header
	}
	return ""
}

func (s *gstate) qcode() *qcode.QCode {
	if s.cs != nil && s.cs.st.qc != nil {
		return s.cs.st.qc
	}
	return nil
}
