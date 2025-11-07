## The z21-gateway

The `z21‑gateway` is a lightweight gateway application that bridges a z21 device to a NATS message bus.

### Getting Started

#### Installation

Clone the repository:

```sh
git clone https://github.com/trains-io/z21-gateway.git
cd z21-gateway
```

#### Building

Build the binary:

```sh
make build
```

#### Running

The `z21-gateway` runs as a standalone binary and connects a z21 device to a NATS message bus. It can be
configured entirely through command-line options or environment variables.

By default, it connects to a local z21 device and NATS instance:

```sh
./build/z21-gateway
```

You can override defaults with flags:

```sh
./build/z21-gateway --z21_addr 192.168.2.6 --nats_url nats://192.168.2.5:4222 --z21_name layout1
```

**Gateway Options:**

- `-zc, --z21_addr <host[:port]>`         z21 address (default: 127.0.0.1:21105)
- `-nc, --nats_url <host>`                NATS server URL (default: nats://127.0.0.1:4222)
- `-n, --name --z21_name <z21_name>`      z21 name (default: main)

**Environment Variables:**

You can also configure gateway using environment variables, which are overridden by command-line
flags if both are set:

- `Z21_NAME` → sets the z21 device address
- `Z21_ADDR` → sets the NATS server URL
- `NATS_URL` → sets the z21 logical name

Output

```sh
9:22PM INF starting Z21 Gateway component=z21gw
9:22PM INF config build=2025-11-07T19:56:02Z component=z21gw context=main nats=nats://127.0.0.1:4222 sha=8993f0b version=v0.1.3-1-g8993f0b-dirty z21=192.168.2.6
9:22PM INF NATS conn component=z21gw url=nats://127.0.0.1:4222
9:22PM DBG Z21 conn component=z21lib from=172.20.236.167:44776 status=connected to=192.168.2.6:21105
9:22PM INF Z21 conn addr=192.168.2.6 component=z21gw context=main
9:22PM DBG starting Z21 heartbeat loop component=z21gw
9:22PM DBG starting Z21 events loop component=z21gw
9:22PM DBG Z21 broadcast sub component=z21gw
9:22PM DBG fire and forget: response tracking disabled component=z21lib
9:22PM DBG [TX] LAN_SET_BROADCASTFLAGS component=z21lib fingerprint=
9:22PM DBG hexdump:
00000000  08 00 50 00 00 01 00 00                           |..P.....| component=z21lib
9:22PM DBG starting NATS commands loop component=z21gw
9:22PM DBG sending hearbeat component=z21gw
9:22PM INF NATS sub component=z21gw subject=z21.main.cmd
9:22PM INF Z21 Gateway started component=z21gw
9:22PM DBG [TX] LAN_GET_SERIAL_NUMBER component=z21lib fingerprint=150c764f
9:22PM DBG hexdump:
00000000  04 00 10 00                                       |....| component=z21lib
9:22PM INF NATS pub component=z21gw reachable=false serial= subject=z21.main.status
```

#### Kubernetes

You can deploy the `z21-gateway` into a local `kind` cluster for testing and development. The provided
`Makefile` automates the entire process - from cluster creation to deployment.

##### Setup

To create a local kind cluster and install a NATS server inside it:

```sh
make setup
```

This target will:

- Create a new kind cluster (if one doesn’t exist)
- Deploy a NATS server for message exchange

Once complete, retrieve the NATS server URL and export it for later use:

```sh
export NATS_URL=nats://$(kubectl get svc nats -o jsonpath='{.spec.clusterIP}:{.spec.ports[0].port}')
```

To discover your z21 device address using the `z21scan` tool:

```sh
export Z21_ADDR=$(z21scan [IFACE|NETWORK] -o short)
```

##### Deployment

Build, containerize, and deploy the gateway into the cluster using:

```sh
make deploy
```

This command will use `ko` to build and publish the image locally, then apply the deployment manifest at
`manifests/z21-gateway.yaml`.

You can override environment variables before deployment to customize the runtime configuration:

```sh
export Z21_NAME=main
export Z21_ADDR=192.168.0.100
export NATS_URL=nats://10.96.0.5:4222
make deploy
```

##### Observing Logs and Events

After deployment, check the gateway’s output:

```sh
kubectl logs deployment/z21-gateway
```

You can inspect messages published by the gateway through NATS server using the `nats-box` utility:

```sh
kubectl exec -it deployment/nats-box -- nats sub z21.main.status
```

This allows you to verify that the gateway is successfully bridging events between your z21 device and the 
NATS message bus inside the cluster.

### License

This project is licensed under the MIT License.

### Contributing

Contributions, bug reports, and feature requests are welcome! Simply open an issue or submit a pull request.
