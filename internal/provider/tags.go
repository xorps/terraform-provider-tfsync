// Copyright (c) HashiCorp, Inc.

package provider

import (
	"fmt"
	"net/url"
	"strings"
)

func newTags(tags map[string]string) string {
	var tagPairs []string

	for k, v := range tags {
		tagPairs = append(tagPairs, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
	}

	return strings.Join(tagPairs, "&")
}
