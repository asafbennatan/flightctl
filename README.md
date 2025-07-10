# Flight Control

> [!NOTE]
> This project is currently in beta.

Flight Control is a service for declarative management of fleets of edge devices and their workloads.

## TLS Configuration

Flight Control supports separate CA bundles for server and client certificate validation, allowing for more flexible certificate management:

- **Server Certificate Validation**: Use `GetServerCABundleX509()` to get CAs for validating server certificates
- **Client Certificate Validation**: Use `GetClientCABundleX509()` to get CAs for validating client certificates (mTLS)

This separation enables scenarios where server certificates are issued by a different Certificate Authority than client certificates, providing enhanced security and operational flexibility.

For more information, please refer to:

* [User Documentation](docs/user/README.md)
* [Developer Documentation](docs/developer/README.md)

<br><br>

[![Watch the demo](docs/images/demo-thumbnail.png)](https://www.youtube.com/watch?v=WzNG_uWnmzk)
