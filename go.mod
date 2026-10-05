module translator-server

go 1.27

require (
	github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.2
	github.com/PuerkitoBio/goquery v1.10.3
	github.com/andybalholm/brotli v1.1.0
	github.com/cloudwego/eino v0.9.21
	github.com/cloudwego/eino-ext/components/model/gemini v0.1.36
	github.com/cloudwego/eino-ext/components/model/openai v0.1.13
	github.com/dop251/goja v0.0.0-20260926152631-39ec2650adc9
	github.com/eino-contrib/jsonschema v1.0.3
	github.com/gorilla/websocket v1.5.3
	github.com/meguminnnnnnnnn/go-openai v0.1.2
	github.com/pocketbase/dbx v1.12.0
	github.com/pocketbase/pocketbase v0.40.4
	golang.org/x/crypto v0.57.0
	golang.org/x/image v0.46.0
	golang.org/x/net v0.59.0
	golang.org/x/sync v0.23.0
	golang.org/x/text v0.42.0
	google.golang.org/genai v1.70.0
	modernc.org/sqlite v1.57.0
)

// Upstream bytedance/sonic refuses to compile on 32-bit platforms and only the
// eino-ext Gemini dependency uses it. This local shim delegates to
// encoding/json (behaviorally identical on Go 1.27, where upstream sonic runs
// in UseStdJSON mode everywhere). See third_party/sonic-compat/sonic.go.
replace github.com/bytedance/sonic => ./third_party/sonic-compat

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	github.com/JohannesKaufmann/dom v0.3.1 // indirect
	github.com/andybalholm/cascadia v1.3.4 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/bytedance/sonic v1.15.0 // indirect
	github.com/cloudwego/eino-ext/libs/acl/openai v0.1.17 // indirect
	github.com/disintegration/imaging v1.6.2 // indirect
	github.com/dlclark/regexp2/v2 v2.7.1 // indirect
	github.com/domodwyer/mailyak/v3 v3.6.2 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/evanphx/json-patch v0.5.2 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/ganigeorgiev/fexpr v0.6.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.4+incompatible // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/pprof v0.0.0-20260902005441-ca85771921e4 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/goph/emperror v0.17.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/nikolalohinski/gonja v1.5.3 // indirect
	github.com/pelletier/go-toml/v2 v2.0.9 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pocketbase/ozzo-validation/v4 v4.3.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/slongfield/pyfmt v0.0.0-20220222012616-ea85ff4c361f // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/yargevad/filepathx v1.0.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/exp v0.0.0-20230713183714-613f0c0eb8a1 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/grpc v1.66.2 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.74.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.0 // indirect
)
