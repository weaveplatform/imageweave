packer {
  required_version = "= 1.16.0"
  required_plugins {
    imageweave = {
      version = "= 0.1.0"
      source  = "github.com/weaveplatform/imageweave"
    }
  }
}
variable "parent_layout" { type = string }
variable "parent_ref" { type = string }
variable "parent_name" { type = string }
variable "parent_digest" { type = string }
variable "source_build" { type = string }
variable "output_directory" { type = string }
variable "timeout" { type = string }
source "imageweave-macos-prepared" "prepared" {
  parent_layout    = var.parent_layout
  parent_ref       = var.parent_ref
  parent_name      = var.parent_name
  parent_digest    = var.parent_digest
  output_directory = var.output_directory
  timeout          = var.timeout
}
build {
  sources = ["source.imageweave-macos-prepared.prepared"]
  post-processor "manifest" {
    output     = "${var.output_directory}/packer-manifest.json"
    strip_path = false
    custom_data = {
      qualification = "unverified"
      source_sha256 = trimprefix(var.parent_digest, "sha256:")
      source_build  = var.source_build
    }
  }
}
