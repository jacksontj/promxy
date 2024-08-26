package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

const headerKey int = 0
const OrgIdKey string = "X-Scope-OrgID"
const batchSizeKey string = "batch-size"

func NewProxyHeaders(h http.Handler, headers []string) *ProxyHeaders {
	return &ProxyHeaders{
		h:       h,
		headers: headers,
	}
}

type ProxyHeaders struct {
	h       http.Handler
	headers []string
}

func (p *ProxyHeaders) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hdrs := make(map[string]string, len(p.headers))
	var ctx = r.Context()
	for _, header := range p.headers {
		if tenants := r.Header.Get(header); tenants != "" {
			hdrs[header] = tenants
		}
	}
	p.h.ServeHTTP(w, r.WithContext(context.WithValue(ctx, headerKey, hdrs)))
}

func GetHeaders(ctx context.Context) map[string]string {
	v := ctx.Value(headerKey)
	if v == nil {
		return nil
	}

	return v.(map[string]string)
}

func splitTenants(tenants string, batchSize int) []string {
	result := make([]string, 0)
	sep := "|"
	if batchSize == 0 {
		result = append(result, tenants)
	} else {
		tenantsList := strings.Split(tenants, sep)
		var batchTenant = ""
		var i = 0
		for _, tenant := range tenantsList {
			if i != 0 {
				batchTenant = batchTenant + sep + tenant
			} else {
				if batchTenant != "" {
					result = append(result, batchTenant)
				}
				batchTenant = tenant
			}
			i = (i + 1) % batchSize
		}
		result = append(result, batchTenant)
	}
	return result
}

func getBatchSize(values map[string]string) int {
	for key, value := range values {
		if key == batchSizeKey {
			i, err := strconv.Atoi(value)
			if err != nil {
				return 0
			} else {
				return i
			}
		}
	}
	return 0
}

func MultipleContexts(ctx context.Context) []context.Context {
	v := ctx.Value(headerKey)
	value := v.(map[string]string)

	resultContexts := make([]context.Context, 0)
	batchSize := getBatchSize(value)
	for key, tenants := range value {
		if key == OrgIdKey {
			tenantsList := splitTenants(tenants, batchSize)
			for _, tenantIds := range tenantsList {
				result := make(map[string]string, 1)
				result[key] = tenantIds
				resultContexts = append(resultContexts, context.WithValue(ctx, headerKey, result))
			}
		}

	}
	return resultContexts
}
