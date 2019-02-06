#!/bin/bash

echo "Running Test for NOT applying CNI Plugin to test pod"
./run_tests_generic.sh iptables/no-rules-iptablestest-lab.yaml TestPodWithNoRules

echo "Running Test for applying CNI Plugin to test pod"
./run_tests_generic.sh iptables/redirect-all-iptablestest-lab.yaml TestPodRedirectsAllPorts
