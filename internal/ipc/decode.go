package ipc

import "strconv"

// UpParamsFromMap extracts optional up/test overrides from a generic params map.
func UpParamsFromMap(m map[string]any) UpParams {
	var p UpParams
	if m == nil {
		return p
	}
	if v, ok := asInt(m["phone_port"]); ok {
		p.PhonePort = v
	}
	if v, ok := asInt(m["local_port"]); ok {
		p.LocalPort = v
	}
	if v, ok := m["proxy_type"].(string); ok {
		p.ProxyType = v
	}
	return p
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(t)
		return n, err == nil
	default:
		return 0, false
	}
}
