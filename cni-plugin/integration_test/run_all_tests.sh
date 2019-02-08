#!/bin/bash

NORMAL=$(tput sgr0)
REVERSE=$(tput smso)
WHITE=$(tput setaf 7)

function header(){
    local msg=$1
    printf "\n${REVERSE}${msg}${NORMAL}\n"
}

function log(){
    local msg=$1
    printf "${WHITE}${msg}${NORMAL}\n"
}

# Helper function to find out when calico.conf/conflist file shows up
doesCalicoExist() {
  # find out if calico is in /etc/cni/net.d/ folder
  simplematch=$(minikube ssh 'ls /etc/cni/net.d | grep calico.conf')
  trimmedmatch=$(echo "$simplematch" | tr -d '[:space:]')
  if [ "$trimmedmatch" != "" ]; then echo "0"; else echo "1"; fi
}

echo "Running this CNI integration test will destroy and recreate your existing minikube installation"
read -p "Are you sure? (y/Y)" kill_minikube
if [ "$kill_minikube" != "y" -a "$kill_minikube" != "Y" ]
then
  exit 1
fi

header "Killing Minikube"
minikube stop
minikube delete
rm -rf ~/.minikube

header "Creating Minikube"
minikube start --kubernetes-version v1.10.8 --memory 8192 --cpus 4 --network-plugin=cni --extra-config=kubelet.network-plugin=cni

header "Building deps and copying images to Minikube"
./../../bin/update-go-deps-shas
./../../bin/build-cli-bin
./../../bin/mkube ./../../bin/docker-build
header "Docker saving"

header "Applying Calico"
kubectl apply -f ./calico-etcd.yaml
kubectl apply -f ./calico.yaml

header "Waiting for Calico components to become ready"
kubectl wait --for=condition=ready pod -n kube-system -l k8s-app=calico-etcd --timeout=30s
kubectl wait --for=condition=ready pod -n kube-system -l k8s-app=calico-kube-controllers --timeout=30s
kubectl wait --for=condition=ready pod -n kube-system -l k8s-app=calico-node --timeout=30s

header "Discover the calico conf file in /etc/cni/net.d"
# adapted from https://superuser.com/questions/878640/unix-script-wait-until-a-file-exists
calico_retry="10" # 10 seconds as default timeout
echo "Find calico.conf/conflist retry countdown starts at $wait_seconds"
sleepy_time="5"
echo "Sleep time between Calico.conf/conflist retries set to $sleepy_time"

doesExist=$(doesCalicoExist)
until [ $calico_retry -eq 0 -o $doesExist = "0" ]
do
  log "Waiting for calico file to appear, $calico_retry retries left"
  sleep $sleepy_time
  doesExist=$(doesCalicoExist)
  calico_retry=$((calico_retry-1))
done

if [ $((wait_seconds)) -eq 0 -a $doesExist = "1" ]
then
  log "could not find calico in /etc/cni/net.d"
  exit 1
else
  log "found calico conf file in /etc/cni/net.d"
fi

header "Applying linkerd-cni plugin"
./../../target/cli/darwin/linkerd install-cni --inbound-port 8080 --outbound-port 8080 --proxy-uid 2102 | kubectl apply -f -

header "Waiting for linkerd-cni components to become ready"
kubectl wait --for=condition=ready pod -n linkerd -l k8s-app=linkerd-cni --timeout=30s

header "Running tests"
./../../bin/mkube ./test_setup.sh
