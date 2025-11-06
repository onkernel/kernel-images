## How to test kernel-images changes locally with docker

- Make relevant changes to kernel-images example adding a new endpoint at `kernel-images/server/cmd/api/api/computer.go`, example I added `SetCursor()` endpoint.
- Run openApi to generate the boilerplate for the new endpoints with make oapi-generate
- Check changes at `kernel-images/server/lib/oapi/oapi.go`
- `cd kernel-images/images/chromium-headful`
-  Build and run the docker image with `./build-docker.sh && ENABLE_WEBRTC=true ./run-docker.sh`
- Open http://localhost:8080/ in your browser
- Now new endpoint should be available for tests example curl command:
```sh
curl -X POST localhost:444/computer/cursor \
  -H "Content-Type: application/json" \
  -d '{"hidden": true}'
```

### How to test kernel-images changes and pushing changes to Unikraft Cloud
- Complete steps above to test the changes locally with docker
- Follow [this guide in Github](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account) to add your ssh keys.
- Add  the followin to your ~/.ssh/config file:
```sh
# ask a colleague for <values>
Host <host-name>
HostName <host-ip>
User <my-username>
IdentityFile ~/.ssh/id_ed25519 # confirm this is same path 
Port <port>
```
- TODO Sayan did something on his side confirm with him this step
- ssh into the VM builder machine with `ssh <my-username>@<host-ip>`
- git clone the kernel-images repo `git clone git@github.com:onkernel/kernel-images.git`
- cd kernel-images/images/chromium-headful
- Set needed environment variables
```sh
export UKC_TOKEN=<your-ukc-token>
export UKC_METRO=<ukc-metro> # example for dev
```
- Run the `build-unikernel.sh`script with a custom image name to avoid being overwritten by someone else
```sh
# It must be prefixed with "onkernel/"
IMAGE=onkernel/foobar:latest ./build-unikernel.sh
```
- You should see an output similar to this one it confirms it was pushed to Unikraft Cloud
```sh
[+] building rootfs via file... done!                                                      x86_64 [0.0s]
[+] packaging index.unikraft.io/onkernel/foobar:latest... done!                kraftcloud/x86_64 [25.7s]
[+] pushing... done!                                                                     2.8 GB [2m 39s]
```
- Image is not ready until you see it with following command, it takes several minutes to show up:
```sh
kraft cloud image list | grep foobar
#expected output:
onkernel/foobar                  latest      2.8 GB
```
- Now you can test it in `api` by setting `UKC_IMAGE` in `kernel/packages/api/.env` to the image name you used in the build script
```sh
UKC_IMAGE=onkernel/foobar:latest
```
- Run `make dev` in `kernel/packages/api` to start the API with the new image
- Test the new endpoint with curl or postman, example:
```sh
curl -X POST http://localhost:3001/browsers/<browser-session-id>/computer/cursor \
  -H 'Authorization: Bearer <JWT-AUTH-TOKEN>' \
  -H 'Content-Type: application/json' \
  -d "{\"hidden\": true}"
```