#!/bin/bash
go build
docker build -t nckl:latest .
