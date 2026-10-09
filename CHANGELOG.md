# Changelog

## 0.1.0 (2026-10-09)


### Features

* add Packer image foundation and native Windows/macOS builders ([46cd22d](https://github.com/weaveplatform/imageweave/commit/46cd22dad001f19c6891edd265b326c8640fbd10))
* add Packer image foundation and native Windows/macOS builders ([f71cc85](https://github.com/weaveplatform/imageweave/commit/f71cc85c97f3a8687a6248b8d5c09116046c645b))
* **build:** plan macOS storage and serialize image jobs ([#15](https://github.com/weaveplatform/imageweave/issues/15)) ([05ddda2](https://github.com/weaveplatform/imageweave/commit/05ddda29e611cf6ffe1d99d54803cb3e87fa5d48))
* **ci:** publish macOS images from a restricted Apple Silicon runner ([#12](https://github.com/weaveplatform/imageweave/issues/12)) ([e44790b](https://github.com/weaveplatform/imageweave/commit/e44790b9a1dfbb3e1219f848e280fb9c80dac3b3))
* deliver native Packer construction as unqualified OCI layouts ([#9](https://github.com/weaveplatform/imageweave/issues/9)) ([a858f8a](https://github.com/weaveplatform/imageweave/commit/a858f8acda9768db6fa7f9bbd1d2911890511879))
* **delivery:** automate Packer candidates and signed OCI verification ([4676fc4](https://github.com/weaveplatform/imageweave/commit/4676fc4370c3e38022cf5e2e06d5ee5e63554ff3))
* **delivery:** verify signed candidates without release-channel bootstrap ([4e1dd73](https://github.com/weaveplatform/imageweave/commit/4e1dd7379882bd98820048c38316f656f8fecead))
* **images:** migrate platform builders and automate OCI candidate delivery ([d92811d](https://github.com/weaveplatform/imageweave/commit/d92811de61519679840c3549c01348c3ba111359))
* **macos:** add standalone base and prepared image builds ([#11](https://github.com/weaveplatform/imageweave/issues/11)) ([180933d](https://github.com/weaveplatform/imageweave/commit/180933d6d8a72eff15660095ae8f794020eabb2e))
* verify protected Linux candidate publication with released OCI ([7f14093](https://github.com/weaveplatform/imageweave/commit/7f14093e4017c0130928f2601d095347baf4e1cd))


### Bug Fixes

* **ci:** correct QEMU device selection and require accelerated acceptance ([f8e8ed8](https://github.com/weaveplatform/imageweave/commit/f8e8ed8a32e08a7456da71442dd552e0a1b9b8a3))
* **ci:** keep macOS Go linker scratch on the host filesystem ([#14](https://github.com/weaveplatform/imageweave/issues/14)) ([19364c4](https://github.com/weaveplatform/imageweave/commit/19364c4d1bf12baeb7a3999b9ffebbc4efddb790))
* **delivery:** consume typed candidate publication metadata ([e68af41](https://github.com/weaveplatform/imageweave/commit/e68af410201ff77827d32e63b1538a918a45649e))
* **macos:** reuse verified IPSWs and budget remaining allocations ([#16](https://github.com/weaveplatform/imageweave/issues/16)) ([0079329](https://github.com/weaveplatform/imageweave/commit/007932915ca758432af11bd6381006dd2483bf58))
* use architecture-compatible Packer seed CD controllers ([a10ed7c](https://github.com/weaveplatform/imageweave/commit/a10ed7cd4afc25603124d5076086ff1ae3e3071c))
* use complete typed metadata for candidate publication ([12ef23e](https://github.com/weaveplatform/imageweave/commit/12ef23ec7ee3414a00b5b700cb5ca095cbbe274d))

## Changelog

release-please maintains this file from conventional commit messages.
