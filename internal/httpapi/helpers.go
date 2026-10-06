package httpapi

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"microwaveplanner/internal/linkbudget"
)

func parsePathInt64(c *gin.Context, name string, dst *int64) (bool, error) {
	raw := c.Param(name)
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false, &linkbudget.ValidationErrors{Errors: []linkbudget.FieldError{{
			Field: name, Message: fmt.Sprintf("must be an integer, got %q", raw),
		}}}
	}
	*dst = v
	return true, nil
}
