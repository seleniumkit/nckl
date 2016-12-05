# Nckl
Nckl (read as "nickel") is a lightweight Selenium load balancer used to transparently distribute browser requests from parallel processes over a limited set of browsers.

## Building
We use [godep](https://github.com/tools/godep) for dependencies management so ensure it's installed before proceeding with next steps. To build the code:

1. Checkout this source tree: ```$ git clone https://github.com/seleniumkit/nckl.git```
2. Download dependencies: ```$ godep restore```
3. Build as usually: ```$ go build```
4. Run compiled binary: ```$GOPATH/bin/nckl```

## Running
Type ```$ nckl --help``` to see all available flags. Usually Nckl is run like the following:
```
$ nckl -users /etc/grid-router/users.properties -quotaDir /etc/grid-router/quota -destination example.com:4444
```
In this command ```-usersFile``` contains path to plain-test users file and ```-quotaDirectory``` contains path to directory with XML quota files. See example ```test-users.properties``` and ```test.xml``` files in [test-data](test-data) directory.

## Docker containers
To build Docker container make sure you have the following installed:

1. [Docker](http://docker.com/) distribution
2. [Go](http://golang.org/) distribution
3. [godep](https://github.com/tools/godep)

To build container type:
```
$ godep restore
$ ./build-container.sh
```
This will build an image ```nckl:latest``` that exposes running Nckl to port 8080.
To run container type:
```
$ docker run -d --name nckl -e DESTINATION='example.com:4444' -v /etc/grid-router/:/etc/grid-router:ro --net host nckl:latest
```
