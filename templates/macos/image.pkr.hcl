packer {
  required_version = "= 1.16.0"
  required_plugins {
    imageweave = {
      version = "= 0.1.0"
      source  = "github.com/weaveplatform/imageweave"
    }
  }
}
variable "release" { type = string }
variable "arch" { type = string }
variable "source_path" { type = string }
variable "source_sha256" { type = string }
variable "source_build" { type = string }
variable "output_directory" { type = string }
variable "timeout" {
  type    = string
  default = "2h"
}

source "imageweave-native" "base" {
  family           = "macos"
  release          = var.release
  arch             = var.arch
  source_path      = var.source_path
  source_sha256    = var.source_sha256
  source_build     = var.source_build
  output_directory = var.output_directory
  timeout          = var.timeout

}
build {
  sources = ["source.imageweave-native.base"]
  post-processor "manifest" {
    output     = "${var.output_directory}/packer-manifest.json"
    strip_path = false
    custom_data = {
      qualification = "unverified"
      source_sha256 = var.source_sha256
      source_build  = var.source_build
    }
  }
}
