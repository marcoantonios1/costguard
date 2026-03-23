package gateway

import "encoding/json"

func rewriteModelInBody(body []byte, newModel string) ([]byte, error) {
	if newModel == "" {
		return body, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	payload["model"] = newModel

	return json.Marshal(payload)
}

func (g *Gateway) compatibleModelForProvider(originalModel, providerName string) string {
	if originalModel == "" || providerName == "" {
		return ""
	}
	if g.modelCompatibility == nil {
		return ""
	}

	perProvider, ok := g.modelCompatibility[originalModel]
	if !ok || perProvider == nil {
		return ""
	}

	return perProvider[providerName]
}
