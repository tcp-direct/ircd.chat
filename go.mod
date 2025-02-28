module git.tcp.direct/ircd/ircd

go 1.17

require (
	code.cloudfoundry.org/bytefmt v0.0.0-20200131002437-cf55d5288a48
	github.com/GehirnInc/crypt v0.0.0-20200316065508-bb7000b8a962
	github.com/docopt/docopt-go v0.0.0-20180111231733-ee0de3bc6815
	github.com/ergochat/confusables v0.0.0-20201108231250-4ab98ab61fb1
	github.com/ergochat/go-ident v0.0.0-20200511222032-830550b1d775
	github.com/ergochat/irc-go v0.0.0-20210617222258-256f1601d3ce
	github.com/go-test/deep v1.0.6 // indirect
	github.com/golang-jwt/jwt v3.2.1+incompatible
	github.com/gorilla/websocket v1.4.2
	github.com/okzk/sdnotify v0.0.0-20180710141335-d9becc38acbd
	github.com/onsi/ginkgo v1.12.0 // indirect
	github.com/onsi/gomega v1.9.0 // indirect
	github.com/stretchr/testify v1.4.0 // indirect
	github.com/tidwall/buntdb v1.2.6
	github.com/toorop/go-dkim v0.0.0-20201103131630-e1cd1a0a5208
	github.com/xdg-go/scram v1.0.2
	golang.org/x/crypto v0.0.0-20210415154028-4f45737414dc
	golang.org/x/text v0.3.6
	gopkg.in/yaml.v2 v2.4.0
)

require git.tcp.direct/ircd/irc-go v0.0.0-20211219221708-8d8346959776

require (
	github.com/tidwall/btree v0.6.0 // indirect
	github.com/tidwall/gjson v1.8.0 // indirect
	github.com/tidwall/grect v0.1.2 // indirect
	github.com/tidwall/match v1.0.3 // indirect
	github.com/tidwall/pretty v1.1.0 // indirect
	github.com/tidwall/rtred v0.1.2 // indirect
	github.com/tidwall/tinyqueue v0.1.1 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	golang.org/x/sys v0.0.0-20201119102817-f84b799fce68 // indirect
	golang.org/x/term v0.0.0-20201126162022-7de9c90e9dd1 // indirect
)

replace github.com/gorilla/websocket => github.com/ergochat/websocket v1.4.2-oragono1

replace github.com/xdg-go/scram => github.com/ergochat/scram v1.0.2-ergo1
