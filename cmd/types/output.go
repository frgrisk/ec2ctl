package types

import (
	"fmt"
	"strings"
)

type Output int

//go:generate stringer -type=Output
const (
	Table Output = iota
	JSON
)

// Set converts a string to the output type
func (i *Output) Set(s string) error {
	for idx := 0; idx < len(_Output_index)-1; idx++ {
		if strings.EqualFold(s, _Output_name[_Output_index[idx]:_Output_index[idx+1]]) {
			*i = Output(idx)
			return nil
		}
	}
	return fmt.Errorf("invalid output type: %q", s)
}

func (i Output) Type() string {
	return "string"
}
