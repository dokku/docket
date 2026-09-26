package tasks

import (
	yaml "gopkg.in/yaml.v3"
)

// TaskExample is a constraint for example structs that have a Name field.
type TaskExample interface {
	GetName() string
}

// MarshalExamples converts a slice of task examples into Doc slices.
func MarshalExamples[T TaskExample](examples []T) ([]Doc, error) {
	var output []Doc
	for _, example := range examples {
		b, err := yaml.Marshal(example)
		if err != nil {
			return nil, err
		}

		output = append(output, Doc{
			Name:      example.GetName(),
			Codeblock: string(b),
		})
	}

	return output, nil
}
