module github.com/RandomLemon/arisu

go 1.25.0

require (
	github.com/RandomLemon/kei v0.0.0
	github.com/RandomLemon/kei-plugin-agent v0.0.0
)

require gopkg.in/yaml.v3 v3.0.1 // indirect

// 两个依赖都有 tag v0.0.1，但都早于当前契约版本（上游 HEAD）：本地开发/构建一律
// replace 到同级检出，发布时再换成真实版本号。
// 插件仓库已改名 kei-plugin-persona，同级检出目录随之变为 ../kei-plugin-persona；
// 其 go.mod 的 module path 仍是 github.com/RandomLemon/kei-plugin-agent，故 require
// 与 import path 都不变（改了会与上游 go.mod 冲突，去掉 replace 发版即失败）。
// kei 是主 module 的直接依赖，插件依赖的 replace 也要在这里写一份：
// Go 只应用主 module 的 replace，被依赖 module 内部的 replace 会被忽略。
replace (
	github.com/RandomLemon/kei => ../kei
	github.com/RandomLemon/kei-plugin-agent => ../kei-plugin-persona
)
