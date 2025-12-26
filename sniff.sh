#!/usr/bin/env bash
sudo tcpdump -n -s 0 -XX 'tcp port 8088 and tcp[13] & 8 != 0'
