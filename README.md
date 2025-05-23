# Simple Content Management System

A flexible content management system that supports multi-backend storage, versioning, and metadata management.

## Features

- Store and manage content with metadata
- Support for multiple storage backends (currently in-memory, with extensibility for file system and S3)
- Content versioning
- Metadata management for both content and objects
- RESTful API for content and object operations

## Getting Started

### Prerequisites

- Go 1.24 or higher

### Installation

1. Clone the repository:

```bash
git clone https://github.com/tendant/simple-content.git
cd simple-content
```

2. Build the application:

```bash
go build -o simple-content ./cmd/server
```

3. Run the server:

```bash
./simple-content
```

The server will start on port 8080 by default. You can change the port by setting the `PORT` environment variable.

## API Usage

### Storage Backends

Before uploading content, you need to create a storage backend:

```bash
# Create a memory storage backend
curl -X POST http://localhost:8080/storage-backend \
  -H "Content-Type: application/json" \
  -d '{
    "name": "memory-backend",
    "type": "memory",
    "config": {}
  }'
```
Response:

```json
{
  "name":"memory-backend",
  "type":"memory",
  "config":{},
  "is_active":true,
  "created_at":"2025-05-23T14:33:59.118817-07:00",
  "updated_at":"2025-05-23T14:33:59.118817-07:00"
}
```

### Content Management

#### Create Content

```bash
# Create a new content
curl -X POST http://localhost:8080/content \
  -H "Content-Type: application/json" \
  -d '{
    "owner_id": "00000000-0000-0000-0000-000000000001",
    "tenant_id": "00000000-0000-0000-0000-000000000001"
  }'
```

Response:

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174000",
  "created_at": "2025-05-21T15:04:05Z",
  "updated_at": "2025-05-21T15:04:05Z",
  "owner_id": "00000000-0000-0000-0000-000000000001",
  "tenant_id": "00000000-0000-0000-0000-000000000001",
  "status": "active"
}
```

#### Add Metadata to Content

```bash
# Add metadata to content
curl -X PUT http://localhost:8080/content/123e4567-e89b-12d3-a456-426614174000/metadata \
  -H "Content-Type: application/json" \
  -d '{
    "filename": "example.txt",
    "description": "An example text file",
    "tags": ["example", "text"]
  }'
```

#### Create an Object for Content

```bash
# Create an object for content (replace storage_backend_name with the name from your storage backend creation)
curl -X POST http://localhost:8080/content/123e4567-e89b-12d3-a456-426614174000/objects \
  -H "Content-Type: application/json" \
  -d '{
    "storage_backend_name": "memory-backend",
    "version": 1
  }'
```

Response:

```json
{
  "id": "123e4567-e89b-12d3-a456-426614174002",
  "content_id": "123e4567-e89b-12d3-a456-426614174000",
  "storage_backend_name": "memory-backend",
  "version": 1,
  "object_key": "123e4567-e89b-12d3-a456-426614174000/123e4567-e89b-12d3-a456-426614174002",
  "status": "pending",
  "created_at": "2025-05-21T15:04:05Z",
  "updated_at": "2025-05-21T15:04:05Z"
}
```

#### Upload Content to Object

```bash
# Upload content to object
curl -X POST http://localhost:8080/object/123e4567-e89b-12d3-a456-426614174002/upload \
  -H "Content-Type: application/octet-stream" \
  --data-binary @example.txt
```

#### Download Content

```bash
# Download content
curl -X GET http://localhost:8080/object/123e4567-e89b-12d3-a456-426614174002/download \
  -o downloaded_example.txt
```

#### Add Metadata to Object

```bash
# Add metadata to object
curl -X PUT http://localhost:8080/object/123e4567-e89b-12d3-a456-426614174002/metadata \
  -H "Content-Type: application/json" \
  -d '{
    "content_type": "text/plain",
    "size": 1024,
    "checksum": "d41d8cd98f00b204e9800998ecf8427e"
  }'
```

### List Operations

#### List Content

```bash
# List all content
curl -X GET http://localhost:8080/content/list

# List content by owner
curl -X GET http://localhost:8080/content/list?owner_id=00000000-0000-0000-0000-000000000001

# List content by tenant
curl -X GET http://localhost:8080/content/list?tenant_id=00000000-0000-0000-0000-000000000001
```

#### List Objects for Content

```bash
# List objects for content
curl -X GET http://localhost:8080/content/123e4567-e89b-12d3-a456-426614174000/objects
```

#### List Storage Backends

```bash
# List all storage backends
curl -X GET http://localhost:8080/storage-backend
```

### Delete Operations

#### Delete Content

```bash
# Delete content (this will also delete all associated objects)
curl -X DELETE http://localhost:8080/content/123e4567-e89b-12d3-a456-426614174000
```

#### Delete Object

```bash
# Delete object
curl -X DELETE http://localhost:8080/object/123e4567-e89b-12d3-a456-426614174002
```

#### Delete Storage Backend

```bash
# Delete storage backend
curl -X DELETE http://localhost:8080/storage-backend/123e4567-e89b-12d3-a456-426614174001
```

## Architecture

The system is designed with a clean architecture approach:

- **Domain Layer**: Core business entities and interfaces
- **Repository Layer**: Data access interfaces and implementations
- **Service Layer**: Business logic and operations
- **API Layer**: HTTP handlers and routes

## Future Enhancements

- Persistent storage with PostgreSQL
- File system storage backend
- S3 storage backend
- Preview generation
- Audit trail implementation
- Event system for lifecycle events

## License

This project is licensed under the MIT License - see the LICENSE file for details.
