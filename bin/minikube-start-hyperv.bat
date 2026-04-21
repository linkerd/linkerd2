REM Starts minikube on Windows 10 using Hyper-V.
REM
REM Windows 10 version 1709 (Creator's Update) or later is required for the
REM "Default Switch (NAT with automatic DHCP).
REM
REM Hyper-V must be enabled in "Turn Windows features on or off."

minikube start --kubernetes-version="v1.8.0" --vm-driver="hyperv" --disk-size=30G --memory=8192 --cpus=4 --hyperv-virtual-switch="Default Switch" --v=7 --alsologtostderr
