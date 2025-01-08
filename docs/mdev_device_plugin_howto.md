# Kubernetes Device Plugin for IBM CryptoExpress (CEX) cards for Kata Containers

## Test Environment

- OS: Ubuntu 22.04.5 LTS
- vCPUs: 4
- Memory: 16G
- Disk: 100G (40G is enough)
- CEX card is attached

## Steps

1. Make sure that a CEX card is attached to a machine:

```bash
$ lszcrypt --verbose
CARD.DOM TYPE  MODE        STATUS     REQUESTS  PENDING HWTYPE QDEPTH FUNCTIONS  DRIVER
--------------------------------------------------------------------------------------------
00       CEX8C CCA-Coproc  online            4        0     14     08 S--D--NF-  cex4card
00.0029  CEX8C CCA-Coproc  online            2        0     14     08 S--D--NF-  cex4queue
00.002a  CEX8C CCA-Coproc  online            2        0     14     08 S--D--NF-  cex4queue
04       CEX8P EP11-Coproc online            6        0     14     08 -----XNF-  cex4card
04.0029  CEX8P EP11-Coproc online            0        0     14     08 -----XNF-  cex4queue
04.002a  CEX8P EP11-Coproc online            6        0     14     08 -----XNF-  cex4queue
```

We configured, deployed, and tested the following steps using the device configuration shown above.
You can adjust the [crypto config set](../deployments/configmap/cex_resources.json) or the k8s
manifest for [docker](../docker-cex-config.yaml) and [kata](../kata-mdev-config.yaml) to suit your environment.

2. Make sure that a device driver for `vfio_ap` is installed:

```bash
$ lsmod | grep vfio_ap
vfio_ap                28672  0
mdev                   28672  2 vfio_ccw,vfio_ap
vfio                   53248  4 vfio_ccw,vfio_iommu_type1,mdev,vfio_ap
```

3. Install kata containers and a kubernetes cluster:

```bash
$ # Clone the project
$ mkdir -p ${HOME}/go/src/github.com/kata-containers
$ cd ${HOME}/go/src/github.com/kata-containers
$ git clone https://github.com/kata-containers/kata-containers.git && cd kata-containers
$ # Build artifacts for kata-deploy
$ make kernel-tarball
$ make rootfs-image-tarball
$ make shim-v2-tarball
$ make virtiofsd-tarball
$ make qemu-tarball
$ mkdir kata-artifacts-bundle
$ build_dir=$(readlink -f build)
$ cp -r $build_dir/*.tar.xz kata-artifacts-bundle
$ ./tools/packaging/kata-deploy/local-build/kata-deploy-merge-builds.sh kata-artifacts-bundle
$ # Make a local docker registry available
$ docker run -d -p 5000:5000 --name local-registry registry:2.8.3
$ # Set up environment variables
$ export INSTALL_K3S_VERSION=v1.28.4+k3s1
$ export KATA_HYPERVISOR=qemu
$ export KUBERNETES=k3s
$ export DOCKER_REPO=build-kata-deploy
$ export DOCKER_REGISTRY=localhost:5000
$ export DOCKER_TAG=latest
$ export TARGET_ARCH=s390x
$ # Build a container image for kata-deploy and push it to the local registry
$ ./tools/packaging/kata-deploy/local-build/kata-deploy-build-and-upload-payload.sh kata-static.tar.xz ${DOCKER_REGISTRY}/${DOCKER_REPO} ${DOCKER_TAG}
$ # Deploy a Kubernetes cluster via k3s
$ ./tests/integration/kubernetes/gha-run.sh deploy-k8s
$ # Deploy kata containers via kata-deploy
$ ./tests/integration/kubernetes/gha-run.sh deploy-kata-zvsi
$ # Verify if a kata containers runs
$ kubectl apply -f ./tests/integration/kubernetes/runtimeclass_workloads/pod-empty-dir.yaml
$ $ kubectl get po
NAME            READY   STATUS    RESTARTS   AGE
sharevol-kata   1/1     Running   0          10s
$ # Delete the test container
$ kubectl delete -f ./tests/integration/kubernetes/runtimeclass_workloads/pod-empty-dir.yaml
```

4. Build a test image for CEX

```bash
$ # Build zcrypttest and locate it on a designated directory
$ git clone https://github.ibm.com/linuxonz/testcases.git
$ pushd testcases/crypto/zcrypttest
$ make
$ mkdir -p ${HOME}/script
$ cp ./zcrypttest ${HOME}/script
$ popd
$ rm -rf testcases
$ # Build and push a test image to the local registry
$ cd ${HOME}/go/src/github.com/kata-containers/kata-containers
$ cat <<EOF > vfio-ap.patch
diff --git a/tests/functional/vfio-ap/run.sh b/tests/functional/vfio-ap/run.sh
index c3bd41eff..0276f7723 100755
--- a/tests/functional/vfio-ap/run.sh
+++ b/tests/functional/vfio-ap/run.sh
@@ -22,10 +22,10 @@ test_image_name="localhost:${registry_port}/vfio-ap-test:latest"

 test_category="[kata][vfio-ap][containerd]"

-trap cleanup EXIT
+#trap cleanup EXIT

 # Prevent the program from exiting on error
-trap - ERR
+#trap - ERR
 registry_image="registry:2.8.3"

 setup_config_file() {
@@ -261,11 +261,11 @@ run_tests() {
 }

 main() {
-    validate_env
-    cleanup
+    #validate_env
+    #cleanup
     build_test_image
-    create_mediated_device
-    run_tests
+    #create_mediated_device
+    #run_tests
 }

 main $@
EOF
$ git apply vfio-ap.patch
$ DEBUG=yes bash -f tests/functional/vfio-ap/run.sh
```

5. Deploy a device plugin and a mutating webhook:

```bash
$ mkdir -p ${HOME}/go/src/github.com/ibm-s390-cloud
$ cd ${HOME}/go/src/github.com/ibm-s390-cloud
$ git clone -b mdev-dp-draft-01 https://github.com/BbolroC/k8s-cex-dev-plugin.git
$ cd k8s-cex-dev-plugin
$ # Deploy a device plugin
$ make RUNTIME=docker build
$ docker push localhost:5000/ibm-cex-plugin-cm:v1.1.3
$ sudo mkdir /var/tmp/shadowsysfs
$ kubectl create -k deployments/rhocp-create
$ # Deploy a mutating webhook
$ pushd deployments/webhook
$ docker build -t localhost:5000/my-webhook:latest .
$ docker push localhost:5000/my-webhook:latest
$ pushd certs
$ kubectl create ns customer-1
$ kubectl create secret tls webhook-certs --cert=tls.crt --key=tls.key -n customer-1
$ popd
$ kubectl apply -f deployment/webhook-deployment.yaml
$ kubectl apply -f deployment/mutating-webhook-config.yaml
$ popd
$ # Configure kata containers to support hot_plug_vfio (device attachment via QMP)
$ config_file="/opt/kata/share/defaults/kata-containers/configuration-qemu.toml"
$ sudo sed -i -e 's/.*\(cold_plug_vfio.*\)/#\1/g' "${config_file}"
$ sudo sed -i -e 's/.*\(hot_plug_vfio\s*=\).*/\1 "bridge-port"/g' "${config_file}"
$ sudo sed -i -e 's/.*\(vfio_mode\s*=\).*/\1 "vfio"/g' "${config_file}"
$ # Run a kata container asking for CCA_for_customer_1
$ kubectl apply -f kata-mdev-config.yaml
$ # Run a docker container asking for EP11_for_customer_1
$ kubectl apply -f docker-cex-config.yaml
```
