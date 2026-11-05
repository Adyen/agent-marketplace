package logvalue

import (
	"fmt"
	"strings"
)

func Safe(value any) any {
	if value == nil {
		return nil
	}
	text := strings.ReplaceAll(fmt.Sprint(value), "\r", `\r`)
	return strings.ReplaceAll(text, "\n", `\n`)
}
