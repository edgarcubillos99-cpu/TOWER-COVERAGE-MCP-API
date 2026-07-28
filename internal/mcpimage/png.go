package mcpimage

import (
	"encoding/base64"

	"github.com/mark3labs/mcp-go/mcp"
)

// PNGToolResult devuelve una captura PNG como contenido image del protocolo MCP.
func PNGToolResult(caption string, png []byte) *mcp.CallToolResult {
	return mcp.NewToolResultImage(caption, base64.StdEncoding.EncodeToString(png), "image/png")
}
