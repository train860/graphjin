module github.com/dosco/graphjin/wasm/v3

go 1.18

replace github.com/dosco/graphjin/conf/v3 => ../conf


require github.com/dosco/graphjin/core/v3 v3.0.0-20230125090031-062b181a2f0f

require (
	github.com/avast/retry-go v3.0.0+incompatible // indirect
	github.com/dosco/graphjin/plugin/osfs/v3 v3.0.0-20230125090031-062b181a2f0f // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/rs/xid v1.4.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/text v0.6.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)