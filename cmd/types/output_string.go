// Code generated by "stringer -type=Output"; DO NOT EDIT.

package types

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Table-0]
	_ = x[JSON-1]
}

const _Output_name = "TableJSON"

var _Output_index = [...]uint8{0, 5, 9}

func (i Output) String() string {
	if i < 0 || i >= Output(len(_Output_index)-1) {
		return "Output(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Output_name[_Output_index[i]:_Output_index[i+1]]
}
