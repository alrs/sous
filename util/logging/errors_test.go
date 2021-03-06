package logging

import (
	"fmt"
	"testing"
)

func TestErrorMessage(t *testing.T) {
	msg := newErrorMessage(fmt.Errorf("just an error"), false)
	AssertMessageFields(t, msg, StandardVariableFields, map[string]interface{}{
		//pkg/errors errors will yield a backtrace here
		"sous-error-backtrace": "just an error",
		"@loglov3-otl":         SousErrorV1,
		"sous-error-msg":       "just an error",
	})
}
