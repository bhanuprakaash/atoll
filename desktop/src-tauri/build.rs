use tauri_build::{AppManifest, Attributes};

fn main() {
    tauri_build::try_build(
        Attributes::new()
            .app_manifest(AppManifest::new().commands(&["check_update", "install_update"])),
    )
    .expect("build Reef")
}
