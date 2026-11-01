package dsh

import "strings"

// deepseekProviderID is the openusage provider whose tile receives usage
// that dsh sent straight to the DeepSeek platform API.
const deepseekProviderID = "deepseek"

// routeProviderAliases maps the provider names dsh records for a request
// route onto openusage provider ids. Names without an entry are used as-is.
var routeProviderAliases = map[string]string{
	"deepseek-official": deepseekProviderID,
	"commandcode":       "command_code",
	"openai-codex":      "codex",
}

// upstreamProvider names the service that hosted a request. dsh logs it as
// the route provider; when that is missing, the response metadata of the
// native DeepSeek client still identifies the platform. Model names are never
// used: dsh reaches the same model family through several hosts (a DeepSeek
// model served by Command Code is billed by Command Code, not DeepSeek).
func upstreamProvider(route, responseKind string) string {
	route = strings.ToLower(strings.TrimSpace(route))
	if alias, ok := routeProviderAliases[route]; ok {
		return alias
	}
	if route != "" {
		return route
	}
	if strings.EqualFold(strings.TrimSpace(responseKind), "deepseek-messages") {
		return deepseekProviderID
	}
	return ""
}

// telemetryProviderID picks the provider id an event is stored under, which
// decides the tile it lands on. Requests hosted by DeepSeek go to the
// deepseek tile, whose own API reports a balance but no usage. Everything
// else stays on the dsh tile: the other hosts' tiles already report
// account-wide usage from their own APIs, and a dsh-only subset would
// replace that breakdown.
func telemetryProviderID(upstream string) string {
	if upstream == deepseekProviderID {
		return deepseekProviderID
	}
	return ID
}
