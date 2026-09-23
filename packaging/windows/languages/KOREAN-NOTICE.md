# Korean installer language

`Korean.isl` is the unmodified official translation bundled with the verified
Inno Setup 6.7.3 compiler, listed at https://jrsoftware.org/files/istrans/.
Its translator credits remain in the file. `korean-source.json` pins the bytes.

The original Inno Setup license is retained in `LICENSE-InnoSetup.txt` and is
also installed under `installer/licenses`. Inno Setup About-box copyright and
website notices are not removed. AgentDock-specific Korean messages are ours
and are maintained separately in `../includes/messages.iss`.

Update the translation and license together from a verified official compiler
package; update hashes only after reviewing changes. Compile and test the actual
Korean install, repair, failure, and uninstall paths. No installer elevation or
ownership decision may depend on translated text.
