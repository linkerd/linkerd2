import * as pulumi from "@pulumi/pulumi";
import * as gcp from "@pulumi/gcp";
import * as k8s from "@pulumi/kubernetes";
const networkname = process.env['NETWORK'] || "dev";
export const scriptNetwork = new gcp.compute.Network(networkname,{
    autoCreateSubnetworks: true
});
const scriptSsh = new gcp.compute.Firewall(networkname+"-allow-ssh", {
    allows: [
        {
            ports: [
                "22"
            ],
            protocol: "tcp",
        },
    ],
    network: scriptNetwork.name,
    priority: 65534,
    description: "allow ssh",
    direction: "INGRESS",
    sourceRanges:["0.0.0.0/0"]
});

const scriptInternal = new gcp.compute.Firewall(networkname+"-allow-internal", {
    allows: [
        {
            ports: [
                "0-65535"
            ],
            protocol: "tcp",
        },
        {
            ports: [
                "0-65535"
            ],
            protocol: "udp",
        },
        {
            protocol: "icmp",
        },
    ],
    network: scriptNetwork.name,
    priority:65534,
    description: "allow internal",
    direction: "INGRESS",
    sourceRanges:["10.128.0.0/9"]
});
