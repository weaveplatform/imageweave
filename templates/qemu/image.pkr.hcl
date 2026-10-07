packer {
  required_version = "= 1.16.0"
  required_plugins {
    qemu = {
      source  = "github.com/hashicorp/qemu"
      version = "= 1.1.7"
    }
  }
}

variable "family" {
  type = string
  validation {
    condition     = contains(["ubuntu", "fedora"], var.family)
    error_message = "Family must be ubuntu or fedora."
  }
}
variable "release" { type = string }
variable "arch" {
  type = string
  validation {
    condition     = contains(["amd64", "arm64"], var.arch)
    error_message = "Arch must be amd64 or arm64."
  }
}
variable "source_url" { type = string }
variable "source_sha256" {
  type = string
  validation {
    condition     = can(regex("^[a-f0-9]{64}$", var.source_sha256))
    error_message = "Source SHA256 must pin authenticated source bytes."
  }
}
variable "source_build" { type = string }
variable "workspace" { type = string }
variable "ssh_private_key_file" {
  type      = string
  sensitive = true
}
variable "ssh_public_key_file" { type = string }
variable "efi_firmware_code" { type = string }
variable "efi_firmware_vars" { type = string }
variable "firmware_code_sha256" { type = string }
variable "firmware_vars_sha256" { type = string }
variable "accelerator" {
  type = string
  validation {
    condition     = contains(["kvm", "hvf", "tcg"], var.accelerator)
    error_message = "Choose kvm, hvf or tcg explicitly for the builder host."
  }
}

source "qemu" "guest" {
  iso_url              = var.source_url
  iso_checksum         = "sha256:${var.source_sha256}"
  disk_image           = true
  format               = "raw"
  disk_size            = "24G"
  disk_interface       = "virtio"
  net_device           = "virtio-net"
  cdrom_interface      = "virtio-scsi"
  qemu_binary          = var.arch == "arm64" ? "qemu-system-aarch64" : "qemu-system-x86_64"
  machine_type         = var.arch == "arm64" ? "virt" : "q35"
  cpu_model            = var.accelerator == "tcg" ? "max" : "host"
  accelerator          = var.accelerator
  cpus                 = 2
  memory               = 4096
  headless             = true
  efi_boot             = true
  efi_firmware_code    = var.efi_firmware_code
  efi_firmware_vars    = var.efi_firmware_vars
  output_directory     = "${var.workspace}/candidate"
  vm_name              = "disk.raw"
  communicator         = "ssh"
  ssh_username         = "imageweave"
  ssh_private_key_file = var.ssh_private_key_file
  ssh_timeout          = "20m"
  shutdown_command     = "sudo bash /tmp/imageweave-seal.sh"
  shutdown_timeout     = "5m"
  cd_label             = "cidata"
  cd_content = {
    "meta-data" = yamlencode({
      instance-id    = "imageweave-build"
      local-hostname = "imageweave-build"
    })
    "user-data" = join("\n", ["#cloud-config", yamlencode({
      ssh_pwauth   = false
      disable_root = true
      users = [{
        name                = "imageweave"
        shell               = "/bin/bash"
        lock_passwd         = true
        sudo                = "ALL=(ALL) NOPASSWD:ALL"
        ssh_authorized_keys = [trimspace(file(var.ssh_public_key_file))]
      }]
    })])
  }
}

build {
  sources = ["source.qemu.guest"]
  provisioner "shell" {
    environment_vars = [
      "IMAGEWEAVE_FAMILY=${var.family}",
      "IMAGEWEAVE_RELEASE=${var.release}",
      "IMAGEWEAVE_ARCH=${var.arch}"
    ]
    script = "${path.root}/../../provision/linux/check.sh"
  }
  provisioner "file" {
    source      = "${path.root}/../../provision/linux/seal.sh"
    destination = "/tmp/imageweave-seal.sh"
  }
  post-processor "manifest" {
    output     = "${var.workspace}/manifest.json"
    strip_path = false
    custom_data = {
      family               = var.family
      release              = var.release
      arch                 = var.arch
      source_build         = var.source_build
      source_sha256        = var.source_sha256
      firmware_code_sha256 = var.firmware_code_sha256
      firmware_vars_sha256 = var.firmware_vars_sha256
      qualification        = "unverified"
      purpose              = "guest-base"
      target               = "qemu"
    }
  }
}
