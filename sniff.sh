#!/usr/bin/env bash
sudo tcpdump -n -i lo0 -s 0 -XX 'tcp port 8088 and tcp[13] & 8 != 0'
