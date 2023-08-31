To compile kubeztl:

```shell
(
set -euxo pipefail
git clone https://github.com/openziti-test-kitchen/kubeztl.git
cd ./kubeztl
mkdir -p ./build
[[ -s ./go.mod ]] || go mod init kubeztl
go mod tidy
go build -o build
)
```
