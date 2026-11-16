package types

import "testing"

func TestOutputSet(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  Output
	}{
		{input: "table", want: Table},
		{input: "TABLE", want: Table},
		{input: "json", want: JSON},
		{input: "JsOn", want: JSON},
	} {
		t.Run(tt.input, func(t *testing.T) {
			var output Output
			if err := output.Set(tt.input); err != nil {
				t.Fatal(err)
			}
			if output != tt.want {
				t.Errorf("output = %v, want %v", output, tt.want)
			}
		})
	}
	for _, input := range []string{"", "yaml", " json"} {
		t.Run("invalid/"+input, func(t *testing.T) {
			output := JSON
			if err := output.Set(input); err == nil {
				t.Fatalf("Set(%q) succeeded", input)
			}
			if output != JSON {
				t.Errorf("invalid input changed output to %v", output)
			}
		})
	}
}
