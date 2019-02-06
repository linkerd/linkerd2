#!/bin/bash

echo "Running Test for NOT applying CNI Plugin to test pod"
./run_tests_generic.sh iptables/no-rules-iptablestest-lab.yaml TestPodWithNoRules
