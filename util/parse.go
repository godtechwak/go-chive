package util

import (
	"fmt"
)

func ParseDuration(duration string) (int, error) {
	var days int
	_, err := fmt.Sscanf(duration, "%dd", &days)
	if err != nil {
		return 0, err
	}
	return days, nil
}
