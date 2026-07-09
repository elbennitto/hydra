#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

image_name="${1:-}"
image_tag="${2:-}"
push_image=false

if [[ "${3:-}" == "--push" ]]; then
  push_image=true
elif [[ -n "${3:-}" ]]; then
  echo "Unsupported argument: ${3}" >&2
  echo "Usage: $0 <hydra|hydra-ci> <registry/image-name:version-tag> [--push]" >&2
  exit 1
fi

if [[ -n "${4:-}" ]]; then
  echo "Too many arguments" >&2
  echo "Usage: $0 <hydra|hydra-ci> <registry/image-name:version-tag> [--push]" >&2
  exit 1
fi

if [[ -z "${image_name}" || -z "${image_tag}" ]]; then
  echo "Usage: $0 <hydra|hydra-ci> <registry/image-name:version-tag> [--push]" >&2
  echo "Example: $0 hydra-ci ghcr.io/hydra-gitops/hydra-ci:v1.2.3" >&2
  echo "Example: $0 hydra ghcr.io/hydra-gitops/hydra:v1.2.3" >&2
  echo "Example: $0 hydra ghcr.io/hydra-gitops/hydra:v1.2.3 --push" >&2
  exit 1
fi

image_name_segment="${image_tag##*/}"
if [[ "${image_name_segment}" != *:* ]]; then
  echo "Image reference must include a version tag: ${image_tag}" >&2
  exit 1
fi

image_name_from_tag="${image_name_segment%%:*}"

version="${image_name_segment##*:}"

context_dir="$(mktemp -d "${TMPDIR:-/tmp}/hydra-container.XXXXXX")"
hydra_go_dir="${repo_root}/hydra-go"
cli_dir="${hydra_go_dir}/cli"
ldflags="-s -w -X hydra-gitops.org/hydra/hydra-go/base/buildinfo.Version=${version}"
tag_sha="$(git -C "${repo_root}" rev-parse --short=12 HEAD 2>/dev/null || true)"
host_arch="$(uname -m)"
target_arch=""
goamd64=""
container_id=""

if ! command -v go >/dev/null 2>&1; then
  echo "go is required to build the container binary" >&2
  exit 1
fi

if [[ -n "${tag_sha}" ]]; then
  ldflags+=" -X hydra-gitops.org/hydra/hydra-go/base/buildinfo.TagSHA=${tag_sha}"
fi

case "${host_arch}" in
  x86_64|amd64)
    target_arch="amd64"
    goamd64="v2"
    ;;
  aarch64|arm64)
    target_arch="arm64"
    ;;
  *)
    echo "Unsupported host architecture: ${host_arch}" >&2
    exit 1
    ;;
esac

dockerfile_path="${repo_root}/tools/build-container-image/Dockerfile"
case "${image_name}" in
  hydra-ci)
    if [[ "${image_name_from_tag}" != *-ci ]]; then
      echo "Image name mismatch: hydra-ci expects an image reference ending in '-ci', got '${image_name_from_tag}'" >&2
      exit 1
    fi
    dockerfile_path="${repo_root}/tools/build-container-image/Dockerfile.ci"
    ;;
  hydra)
    if [[ "${image_name_from_tag}" == *-ci ]]; then
      echo "Image name mismatch: hydra expects a runtime image reference without '-ci', got '${image_name_from_tag}'" >&2
      exit 1
    fi
    ;;
  *)
    echo "Unsupported image name: ${image_name} (expected hydra or hydra-ci)" >&2
    exit 1
    ;;
esac

cleanup() {
  if [[ -n "${container_id}" ]]; then
    docker rm -f "${container_id}" >/dev/null 2>&1 || true
  fi
  rm -rf "${context_dir}"
}
trap cleanup EXIT

mkdir -p "${context_dir}/linux/${target_arch}"

echo "Building static linux/${target_arch} container binary for host arch ${host_arch}..."
if [[ -n "${goamd64}" ]]; then
  CGO_ENABLED=0 GOOS=linux GOARCH="${target_arch}" GOAMD64="${goamd64}" \
    go build -C "${cli_dir}" -ldflags "${ldflags}" -o "${context_dir}/linux/${target_arch}/hydra" .
else
  CGO_ENABLED=0 GOOS=linux GOARCH="${target_arch}" \
    go build -C "${cli_dir}" -ldflags "${ldflags}" -o "${context_dir}/linux/${target_arch}/hydra" .
fi

docker build \
  -f "${dockerfile_path}" \
  --build-arg "TARGETOS=linux" \
  --build-arg "TARGETARCH=${target_arch}" \
  --build-arg "VERSION=${version}" \
  -t "${image_tag}" \
  "${context_dir}"

container_id="$(docker create "${image_tag}")"

echo
echo "Contents of ${image_tag}:"
docker export "${container_id}" | tar -tvf -

if [[ "${push_image}" == "true" ]]; then
  echo
  echo "Pushing ${image_tag}..."
  docker push "${image_tag}"
fi
