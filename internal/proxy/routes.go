package proxy

// route 登记一条对外路由契约（方法 + 路径）。
type route struct {
	method string
	path   string
}

// routeTable 是一组路由契约，附带方法白名单判定。
type routeTable []route

// routes 是代理对外暴露的路由集合：五个方法共用同一个 catch-all 处理器，
// 未登记的方法按 HTTP 语义回 501（对齐旧实现 BaseHTTPRequestHandler 的默认行为）。
//
// 它同时是 docs/API.md 与 scripts/check-api-docs.mjs 的唯一来源：改路由必须同步文档。
var routes = routeTable{
	{method: "GET", path: "/*"},
	{method: "POST", path: "/*"},
	{method: "PUT", path: "/*"},
	{method: "DELETE", path: "/*"},
	{method: "HEAD", path: "/*"},
}

// allows 判定方法是否在路由表内。
func (rt routeTable) allows(method string) bool {
	for _, r := range rt {
		if r.method == method {
			return true
		}
	}
	return false
}
