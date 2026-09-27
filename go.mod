module github.com/RandomLemon/kei/arisu

go 1.25.0

require (
	github.com/RandomLemon/kei v0.0.0
	github.com/RandomLemon/kei-plugin-agent v0.0.0
)

require (
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// 两个依赖都没有 release tag，本地开发/构建一律指向同级检出。
// kei 是主 module 的直接依赖，kei-plugin-agent 的 replace 也要在这里写一份：
// Go 只应用主 module 的 replace，被依赖 module 内部的 replace 会被忽略。
replace (
	github.com/RandomLemon/kei => ../kei
	github.com/RandomLemon/kei-plugin-agent => ../kei-plugin-agent
)
