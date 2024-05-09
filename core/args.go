package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

// argList function is used to create a list of arguments to pass
// to a prepared statement.

type args struct {
	json   []byte
	values []interface{}
	cindx  int // index of cursor arg
}

func (gj *graphjin) argList(c context.Context,
	st stmt,
	md psql.Metadata,
	fields map[string]json.RawMessage,
	rc *ReqConfig,
	buildJSON bool,
) (ar args, err error) {
	ar = args{cindx: -1}
	params := md.Params()
	vl := make([]interface{}, len(params))

	for i, p := range params {
		switch p.Name {
		case "user_id", "userID", "userId":
			if v := c.Value(UserIDKey); v != nil {
				switch v1 := v.(type) {
				case string:
					vl[i] = v1
				case int:
					vl[i] = v1
				case float64:
					vl[i] = int(v1)
				default:
					return ar, fmt.Errorf("%s must be an integer or a string: %T", p.Name, v)
				}
			} else {
				return ar, argErr(p)
			}

		case "user_id_raw", "userIDRaw", "userIdRaw":
			if v := c.Value(UserIDRawKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "user_id_provider", "userIDProvider", "userIdProvider":
			if v := c.Value(UserIDProviderKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "user_role", "userRole":
			if v := c.Value(UserRoleKey); v != nil {
				vl[i] = v.(string)
			} else {
				return ar, argErr(p)
			}

		case "cursor":
			if v, ok := fields["cursor"]; ok && v[0] == '"' {
				vl[i] = string(v[1 : len(v)-1])
			} else {
				vl[i] = nil
			}
			ar.cindx = i

		default:
			if v, ok := fields[p.Name]; ok {
				varIsNull := bytes.Equal(v, []byte("null"))
				switch {
				case p.IsNotNull && varIsNull:
					return ar, fmt.Errorf("variable '%s' cannot be null", p.Name)

				case p.IsArray && v[0] != '[' && !varIsNull:
					return ar, fmt.Errorf("variable '%s' should be an array of type '%s'", p.Name, p.Type)

				case p.Type == "json" && v[0] != '[' && v[0] != '{' && !varIsNull:
					return ar, fmt.Errorf("variable '%s' should be an array or object", p.Name)
				}
				vl[i] = parseVarVal(v)

			} else if rc != nil {
				if v, ok := rc.Vars[p.Name]; ok {
					switch v1 := v.(type) {
					case (func() string):
						vl[i] = v1()
					case (func() int):
						vl[i] = v1()
					case (func() bool):
						vl[i] = v1()
					default:
						vl[i] = v
					}
				}
			} else {
				return ar, argErr(p)
			}
		}
	}
	//强制给args注入自动列

	qc := st.qc
	newValues := make([]interface{}, len(vl))
	tables := make([]string, 0)

	for _, v := range qc.Mutates {
		tables = append(tables, v.Key)
	}
	if qc.SType == qcode.QTInsert || qc.SType == qcode.QTUpdate {
		autoColumnMap := make(map[string]qcode.AutoColumn)
		for _, v := range qc.AutoColumns {
			if !slices.Contains(v.QTypes, qc.SType) {
				continue
			}
			if v.Value == "" && v.ValueFn == nil {
				continue
			}
			autoColumnMap[v.Name] = *v
		}
		for i, v := range vl {
			if err := json.Unmarshal(v.(json.RawMessage), &newValues[i]); err != nil {
				continue
			}
			addAutoColumn2Arg(qc, newValues[i], tables, autoColumnMap, tables[0])
		}
		ar.values = newValues
	} else {
		ar.values = vl
	}

	if buildJSON && len(vl) != 0 {
		if ar.json, err = json.Marshal(vl); err != nil {
			return
		}
	}
	return ar, nil
}

func parseVarVal(v json.RawMessage) interface{} {
	switch v[0] {
	case '[', '{':
		return v

	case '"':
		return string(v[1 : len(v)-1])

	case 't', 'T':
		return true

	case 'f', 'F':
		return false

	case 'n':
		return nil

	default:
		return string(v)
	}
}

func argErr(p psql.Param) error {
	return fmt.Errorf("required variable '%s' of type '%s' must be set", p.Name, p.Type)
}
func addAutoColumn2Arg(qc *qcode.QCode, arg interface{}, tables []string, autoColumnMap map[string]qcode.AutoColumn, key string) {
	switch arg := arg.(type) {

	case map[string]interface{}:

		for k, v := range arg {
			valueType := reflect.TypeOf(v)

			if valueType.Kind() == reflect.Map || valueType.Kind() == reflect.Slice {
				//k转为下划线
				snakeKey := util.ToSnake(k)
				if !slices.Contains(tables, snakeKey) {
					continue
				}

				addAutoColumn2Arg(qc, v, tables, autoColumnMap, snakeKey)

			}
		}
		autoValue := make(map[string]string)
		for kk, vv := range autoColumnMap {
			mValue := vv.Value
			if vv.ValueFn != nil {
				mValue = vv.ValueFn()
			}
			autoValue[kk] = mValue
			arg[kk] = mValue
		}
		if len(autoValue) > 0 {
			if qc.AutoValues[key] == nil {
				qc.AutoValues[key] = make([]map[string]string, 0)
			}
			qc.AutoValues[key] = append(qc.AutoValues[key], autoValue)
		}
	case []interface{}:

		for _, v := range arg {
			addAutoColumn2Arg(qc, v, tables, autoColumnMap, key)
		}
	}
}
