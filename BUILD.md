To compile kubeztl:
```powershell
git clone https://github.com/openziti-test-kitchen/kubeztl.git
cd kubeztl
mkdir build
go mod init kubeztl
go mod tidy
go build -o build
```