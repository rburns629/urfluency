module example.com/app

go 1.22

toolchain go1.22.5

require github.com/gin-gonic/gin v1.9.0

require (
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/text v0.3.7
	example.com/local v1.0.0
	github.com/old/mod v1.2.0
	"github.com/quoted/mod" v0.0.0-20210101000000-abcdef123456 // indirect
)

replace example.com/local => ../local

replace (
	github.com/old/mod v1.2.0 => github.com/new/mod v1.3.0
	golang.org/x/text => golang.org/x/text v0.3.8
)

exclude golang.org/x/net v0.16.0
