# Docker Setup and Running Instructions

This document provides instructions on how to set up and run the application using Docker and Docker Compose.

## Prerequisites

Before you begin, ensure you have the following installed on your system:

*   Docker: Follow the official installation guide for your operating system.
*   Docker Compose: Follow the official installation guide for your operating system.

## Setup

1.  **Environment Variables:**
    Create a `.env` file in the same directory as the `docker-compose.yaml` file. This file will contain the environment variables required by the application. Add the following content to the `.env` file:

    ```env
    KITE_API_KEY=YOUR_KITE_API_KEY
    KITE_API_SECRET=YOUR_KITE_API_SECRET
    APP_MODE=sse
    APP_HOST=0.0.0.0
    APP_PORT=8080
    ```

    **Replace `YOUR_KITE_API_KEY` and `YOUR_KITE_API_SECRET` with your actual Kite API credentials.**

2.  **Dockerfile:**
    Ensure a `Dockerfile` exists in the same directory as the `docker-compose.yaml` file. This file contains the instructions to build the Docker image.

## Running the Application

1.  **Build and Run:**
    Open your terminal, navigate to the directory containing the `docker-compose.yaml` and `.env` files, and run the following command:

    ```bash
    docker compose up --build -d
    ```

    *   `--build`: This flag tells Docker Compose to build the images before starting the containers.
    *   `-d`: This flag runs the containers in detached mode (in the background).

2.  **Stopping the Application:**
    To stop the running containers, use the following command in the same directory:

    ```bash
    docker compose down
    ```

## Accessing the Application

The application should be accessible on `http://localhost:8080` (or the IP address of your server if not running locally) after the containers are up and running. 