package core

import (
	"encoding/json"
	"slices"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

func (s *gstate) compileAutoColumn(st stmt) {
	//add auto columns
	typeMap := map[qcode.MType]qcode.QType{
		qcode.MTInsert: qcode.QTInsert,
		qcode.MTUpdate: qcode.QTUpdate,
		qcode.MTDelete: qcode.QTDelete,
		qcode.MTUpsert: qcode.QTUpsert,
	}
	for i, m := range st.qc.Mutates {
		colMap := make(map[string]int)
		for i, c := range m.Cols {
			colMap[c.FieldName] = i
		}
		values := make(map[string]interface{})
		for _, v := range s.gj.conf.AutoColumns {
			if !slices.Contains(v.QTypes, typeMap[m.Type]) {
				continue
			}
			col := m.Ti.GetTableColumn(v.Name)
			mColumn := qcode.MColumn{
				Col:       *col,
				Alias:     col.Name,
				FieldName: col.Name,
				Value:     v.Value,
				Set:       false,
			}
			if v.ValueFn != nil {
				mColumn.Value = v.ValueFn()
			}

			index, ok := colMap[col.Name]
			if !ok && (v.Rule == qcode.ColumnInsert || v.Rule == qcode.ColumnUpsert) {
				st.qc.Mutates[i].Cols = append(st.qc.Mutates[i].Cols, mColumn)
			} else if v.Rule == qcode.ColumnUpdate || v.Rule == qcode.ColumnUpsert {
				st.qc.Mutates[i].Cols[index] = mColumn
			} else {
				continue
			}
			m.Data.CMap[col.Name] = &graph.Node{
				Type: graph.NodeStr,
				Name: col.Name,
				Val:  mColumn.Value,
			}
			values[col.Name] = mColumn.Value
		}
	}
	s.updateVars(st, typeMap)
}

func (s *gstate) updateVars(st stmt, typeMap map[qcode.MType]qcode.QType) error {
	vars := st.qc.ActionVal

	var vmap interface{}
	if err := json.Unmarshal(vars, &vmap); err != nil {
		return err
	}
	for _, m := range st.qc.Mutates {
		for _, v := range s.gj.conf.AutoColumns {
			if !slices.Contains(v.QTypes, typeMap[m.Type]) {
				continue
			}
			col := m.Ti.GetTableColumn(v.Name)
			s.updateValue(vmap, m.Path, col.Name, v.ValueFn)
		}
	}
	vars, err := json.Marshal(vmap)
	if err != nil {
		return err
	}
	st.qc.ActionVal = vars
	s.vmap[st.qc.ActionVar] = vars
	return nil
}
func (s *gstate) updateValue(data interface{}, path []string, key string, valueFn func() string) {
	switch data.(type) {
	case map[string]interface{}:
		if len(path) == 0 {
			data.(map[string]interface{})[key] = valueFn()
			return
		}
		for i, p := range path {
			if s.gj.conf.EnableCamelcase {
				p = util.ToCamel(p)
			}
			v, ok := data.(map[string]interface{})[p]
			if !ok {
				break
			}
			switch v.(type) {
			case map[string]interface{}:
				v.(map[string]interface{})[key] = valueFn()
			case []interface{}:
				for _, d := range v.([]interface{}) {
					s.updateValue(d, path[i+1:], key, valueFn)
				}
			}
		}
	case []interface{}:
		for _, d := range data.([]interface{}) {
			s.updateValue(d, path, key, valueFn)
		}
	}
}
