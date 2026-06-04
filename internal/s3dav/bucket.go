package s3dav

import (
	"fmt"
	"regexp"
)

var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

func validateBucketName(name string) error {
	if !bucketNamePattern.MatchString(name) {
		return fmt.Errorf("bucket name must be 3-63 characters of lowercase letters, numbers, dots, or hyphens")
	}
	return nil
}
