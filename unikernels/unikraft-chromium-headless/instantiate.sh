#!/bin/sh

kraft cloud inst create \
 --start \
 --name chromium-headless \
 --subdomain $1 \
 --vcpus 1 \
 -M 1.5Gi \
 -p 443:8080/http+tls \
 onkernel/chromium-headless:latest
