module localclash

go 1.24

require (
	github.com/andybalholm/brotli v1.1.1
	github.com/coder/websocket v1.8.15
	google.golang.org/protobuf v1.36.8
	gopkg.in/yaml.v3 v3.0.1
)

// Bound the v3.0.1 emitter queue for large generated configs (go-yaml/yaml#873).
replace gopkg.in/yaml.v3 => github.com/vjorlikowski/yaml/v3 v3.0.2-0.20220714132059-a24d571ad76b
