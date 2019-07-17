# pulumi-stuff

Uses clusters.makefile to create VPC and kubernetes cluster in GCP using pulumi (other functionalities haven't been ported to pulumi yet).

First time execution: 

make -f clusters.makefile bootstrap
(installs dependencies)

To create network:

make -f clusters.makefile create-network
(currently asks for permission before creating)

To create cluster:

make -f clusters.makefile create
(currently asks for permission before creating)

To delete cluster:

make -f clusters.makefile delete
(currently asks for permission before destroying)

NOTE: just to be safe, and to avoid unintentionally spending all your GCP credits, cd into each directory (gcp-network and gcp-cluster) and manually execute the command: pulumi destroy
