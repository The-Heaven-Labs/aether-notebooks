package api

import (
	"fmt"
	"regexp"
	"strings"
)

var slugRefRe = regexp.MustCompile(`\{\{([a-zA-Z0-9_-]+)\}\}`)

func resolveSlugRefs(source string, slugMap map[string]string, knownParams map[string]bool) (string, error) {
	return resolveWithVisited(source, slugMap, knownParams, []string{})
}

func resolveWithVisited(source string, slugMap map[string]string, knownParams map[string]bool, visiting []string) (string, error) {
	matches := slugRefRe.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return source, nil
	}

	result := source
	for _, m := range matches {
		token := m[0]
		slug := m[1]

		// Parameter references pass through unchanged — the executor handles them.
		if knownParams[slug] {
			continue
		}

		refSource, ok := slugMap[slug]
		if !ok {
			return "", fmt.Errorf("unknown slug %q referenced in query", slug)
		}

		for _, v := range visiting {
			if v == slug {
				cycle := append(visiting, slug)
				return "", fmt.Errorf("cycle detected in slug references: %s", strings.Join(cycle, " → "))
			}
		}

		resolved, err := resolveWithVisited(refSource, slugMap, knownParams, append(visiting, slug))
		if err != nil {
			return "", err
		}

		result = strings.Replace(result, token, "("+resolved+")", 1)
	}
	return result, nil
}
