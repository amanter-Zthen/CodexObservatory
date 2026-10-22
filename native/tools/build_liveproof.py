"""Build a self-contained live inspection page. Requires Go + Python, no npm.
The Windows executable does not load this page, browser, or Go/WASM runtime.
"""
from pathlib import Path
import base64, os, subprocess, argparse, tempfile
root=Path(__file__).resolve().parents[1]
p=argparse.ArgumentParser()
p.add_argument('--out',default=str(root.parent/'verification/native-liveproof.html'))
p.add_argument('--go',default='go')
args=p.parse_args()
env=dict(os.environ,GOOS='js',GOARCH='wasm',GOTOOLCHAIN='local')
with tempfile.TemporaryDirectory() as tmp:
    wasm=Path(tmp)/'proof.wasm'
    subprocess.run([args.go,'build','-modfile=monitor-offline.mod','-trimpath','-ldflags=-s -w','-o',str(wasm),'./cmd/liveproof'],cwd=root,env=env,check=True)
    goroot=Path(subprocess.check_output([args.go,'env','GOROOT'],env=dict(os.environ,GOTOOLCHAIN='local'),text=True).strip())
    runtimes=[goroot/'lib/wasm/wasm_exec.js',goroot/'misc/wasm/wasm_exec.js']
    runtime=next(x for x in runtimes if x.exists()).read_text()
    text=(root/'tools/liveproof.template.html').read_text().replace('/*__GO_RUNTIME__*/',runtime).replace('/*__WASM_BASE64__*/',base64.b64encode(wasm.read_bytes()).decode())
    dest=Path(args.out).resolve();dest.parent.mkdir(parents=True,exist_ok=True);dest.write_text(text,encoding='utf-8');print(dest)
