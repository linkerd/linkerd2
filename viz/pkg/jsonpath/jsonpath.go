package jsonpath

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/client-go/util/jsonpath"
)

var jsonRegexp = regexp.MustCompile(`^\{\.?([^{}]+)\}$|^\.?([^{}]+)$`)

func GetJsonPathFlagVal(flagVal string) (string, error) {
	val := strings.Split(flagVal, "=")
	if len(val) != 2 || len(val) == 2 && val[1] == "" {
		return "", errors.New("{\"error jsonpath\": \"jsonpath filter not found\"}")
	}

	return val[1], nil

}

// GetJSONPathExpression get jsonpath filter from flag and attempts to be flexible with JSONPath expressions, it accepts:
//   - metadata.name (no leading '.' or curly braces '{...}'
//   - {metadata.name} (no leading '.')
//   - .metadata.name (no curly braces '{...}')
//   - {.metadata.name} (complete expression)
//
// And transforms them all into a valid jsonpath expression:
//
//	{.metadata.name}
func GetFormatedJSONPathExpression(pathExpression string) (string, error) {
	if len(pathExpression) == 0 {
		return pathExpression, nil
	}
	submatches := jsonRegexp.FindStringSubmatch(pathExpression)
	if submatches == nil {
		return "", errors.New("{\"error jsonpath\": \"unexpected path string, expected a 'name1.name2' or '.name1.name2' or '{name1.name2}' or '{.name1.name2}'\"}")
	}
	if len(submatches) != 3 {
		return "", fmt.Errorf("{\"error jsonpath\":\"unexpected submatch list: %v\"", submatches)
	}
	var fieldSpec string
	if len(submatches[1]) != 0 {
		fieldSpec = submatches[1]
	} else {
		fieldSpec = submatches[2]
	}
	return fmt.Sprintf("{.%s}", fieldSpec), nil
}

func GetJsonFilteredByJPath(event interface{}, jsonPath string) ([]string, error) {
	fields, err := GetFormatedJSONPathExpression(jsonPath)
	if err != nil {
		return []string{}, err
	}

	j := jsonpath.New("EventParser")
	if err := j.Parse(fields); err != nil {
		return []string{}, fmt.Errorf("{\"error parsing jsonpath\":\" %s\" }", err.Error())
	}

	results, err := j.FindResults(event)
	if err != nil {
		return []string{}, fmt.Errorf("{\"error jsonpath\":\" %s\" }", err.Error())
	}

	filteredEvent := []string{}
	if len(results) == 0 || len(results[0]) == 0 {
		return filteredEvent, errors.New("{\"error filtering JSON\": \"couldn't find any results matching with jsonpath filter\"}")
	}

	for _, result := range results {
		for _, match := range result {
			e, err := json.MarshalIndent(match.Interface(), "", "  ")
			if err != nil {
				return []string{}, fmt.Errorf("{\"error marshalling JSON\": \"%s\"}", err.Error())
			}
			filteredEvent = append(filteredEvent, string(e))
		}
	}

	return filteredEvent, nil
}
