#!/bin/bash

# Script to manage the OpenTelemetry Collector container

CONTAINER_NAME="otel-collector"
IMAGE_NAME="otel-collector:latest"

case "$1" in
    start)
        echo "Starting OpenTelemetry Collector..."
        podman run -d --name $CONTAINER_NAME -p 4317:4317 -p 8889:8889 $IMAGE_NAME
        echo "Container started. Check logs with: podman logs $CONTAINER_NAME"
        ;;
    stop)
        echo "Stopping OpenTelemetry Collector..."
        podman stop $CONTAINER_NAME
        podman rm $CONTAINER_NAME
        echo "Container stopped and removed."
        ;;
    restart)
        echo "Restarting OpenTelemetry Collector..."
        $0 stop
        sleep 2
        $0 start
        ;;
    logs)
        podman logs -f $CONTAINER_NAME
        ;;
    status)
        podman ps --filter name=$CONTAINER_NAME
        ;;
    build)
        echo "Building OpenTelemetry Collector image..."
        podman build -t $IMAGE_NAME .
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|logs|status|build}"
        echo ""
        echo "Commands:"
        echo "  start   - Start the collector container"
        echo "  stop    - Stop and remove the collector container"
        echo "  restart - Restart the collector container"
        echo "  logs    - Show container logs"
        echo "  status  - Show container status"
        echo "  build   - Build the container image"
        exit 1
        ;;
esac 