# GitHub Workflows

This repository uses simple GitHub Actions workflows for CI/CD.

## Workflows

### CI Workflow (`ci.yml`)
- **Triggers**: On push to main and pull requests
- **Purpose**: Run tests and verify build
- **Steps**:
  1. Checkout code
  2. Setup Go 1.24
  3. Run tests
  4. Build binary

### Docker Workflow (`docker.yml`)
- **Triggers**: On push to main and version tags (v*)
- **Purpose**: Build and push multi-arch Docker images
- **Steps**:
  1. Checkout code
  2. Setup Docker buildx for multi-arch
  3. Login to GitHub Container Registry
  4. Build and push images (linux/amd64, linux/arm64)
  5. Tags:
     - `latest` - always updated on main branch
     - `vX.Y.Z` - created when pushing version tags

## Usage

### Running Tests Locally
```bash
make test
```

### Building Docker Image Locally
```bash
make docker
```

### Creating a Release
1. Tag your commit: `git tag v1.2.3`
2. Push the tag: `git push origin v1.2.3`
3. The Docker workflow will automatically build and push the versioned image

## Notes

- Linting is available locally via `make lint` but not enforced in CI
- Multi-arch Docker images are built for both AMD64 and ARM64
- Images are published to GitHub Container Registry (ghcr.io)