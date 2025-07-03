#!/usr/bin/env bash

source common.sh
(cd image && docker build -t $IMAGE .)
