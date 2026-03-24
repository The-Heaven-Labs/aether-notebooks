package executor

import "strings"

func ResolveParams(query string, params map[string]string) string {
	for k, v := range params {
		query = strings.ReplaceAll(query, "{{"+k+"}}", v)
	}
	return query
}
