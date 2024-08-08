package qcode

import (
	"bytes"
	"strconv"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/util"
)

func (co *Compiler) ParseName(name string) string {
	if co.c.EnableCamelcase {
		return util.ToSnake(name)
	}
	return name
}

func GetQType(t graph.ParserType) QType {
	switch t {
	case graph.OpQuery:
		return QTQuery
	case graph.OpSub:
		return QTSubscription
	case graph.OpMutate:
		return QTMutation
	default:
		return QTUnknown
	}
}

func GetQTypeByName(t string) QType {
	switch t {
	case "query":
		return QTQuery
	case "subscription":
		return QTSubscription
	case "mutation":
		return QTMutation
	default:
		return QTUnknown
	}
}

func graphNodeToJSON(node *graph.Node, w *bytes.Buffer) {
	switch node.Type {
	case graph.NodeStr:
		w.WriteString(`"` + node.Val + `"`)

	case graph.NodeNum, graph.NodeBool:
		w.WriteString(node.Val)

	case graph.NodeObj:
		w.WriteString(`{`)
		for i, c := range node.Children {
			if i == 0 {
				w.WriteString(`"` + c.Name + `": `)
			} else {
				w.WriteString(`,"` + c.Name + `": `)
			}
			graphNodeToJSON(c, w)
		}
		w.WriteString(`}`)

	case graph.NodeList:
		w.WriteString(`[`)
		for i, c := range node.Children {
			if i != 0 {
				w.WriteString(`,`)
			}
			graphNodeToJSON(c, w)
		}
		w.WriteString(`]`)
	}
}

func interface2Str(v interface{}) string {
	if v == nil {
		return ""
	}
	switch v1 := v.(type) {
	case string:
		return v1
	case []byte:
		return string(v1)
	case int:
		return strconv.Itoa(v1)
	case int64:
		return strconv.FormatInt(v1, 10)
	case float64:
		return strconv.FormatFloat(v1, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v1)
	case float32:
		return strconv.FormatFloat(float64(v1), 'f', -1, 32)
	}
	return ""
}
