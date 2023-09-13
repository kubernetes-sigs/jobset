package names

import (
	"fmt"
)

func GenJobName(jsName, rjobName string, jobIndex int) string {
	return fmt.Sprintf("%s-%s-%d", jsName, rjobName, jobIndex)
}
