#!/bin/bash

name="kernel-cu"

kraft cloud deploy \
	-M 8192 \
	-p 443:6080/http+tls \
    -p 9222:9222/tls \
	-e DISPLAY_NUM=1 \
	-e HEIGHT=768 \
	-e WIDTH=1024 \
	-e HOME=/ \
	-n "$name" \
	.
