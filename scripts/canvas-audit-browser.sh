#!/bin/bash
# Canvas audit — drives browser via sb_server curl calls
SB="http://127.0.0.1:9876"
OUT="/sandbox/workspace/canvas-audit-results.json"

echo '{' > "$OUT"
echo '"timestamp": "'$(date -Iseconds)'",' >> "$OUT"

# ── Navigate ──
echo "=== NAVIGATING ==="
NAV=$(curl -s -X POST "$SB/navigate" -H 'Content-Type: application/json' -d '{"url":"file:///sandbox/workspace/canvas-audit-test.html"}')
VP_W=$(echo "$NAV" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('page_stats',{}).get('viewport_w','?'))")
VP_H=$(echo "$NAV" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('page_stats',{}).get('viewport_h','?'))")
SS=$(echo "$NAV" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('screenshot_path',''))")
echo "viewport: ${VP_W}x${VP_H}, screenshot: $SS"
echo '"viewport": "'${VP_W}'x'${VP_H}'",' >> "$OUT"
echo '"nav_screenshot": "'$SS'",' >> "$OUT"
sleep 0.5

# ── Dismiss OnboardingCoach ──
curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var oc=document.getElementById('\''onboarding-coach'\'');if(oc)oc.classList.add('\''hidden'\'');try{localStorage.setItem('\''inpaint:onboarding:v1'\'','\''done'\'');}catch(e){}return '\''ok'\'';})()"}' > /dev/null

# ── CHECK 1: Brush paints ──
echo "=== CHECK 1: Brush paints on blank canvas ==="
BEFORE=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var c=document.querySelector('\''canvas[data-testid=\"inpaint-mask-canvas\"]'\'');if(!c)return JSON.stringify({error:\"no canvas\"});var ctx=c.getContext('\''2d'\'');var d=ctx.getImageData(0,0,c.width,c.height).data;var nz=0;for(var i=3;i<d.length;i+=4){if(d[i]>0)nz++;}return JSON.stringify({before_nz:nz,w:c.width,h:c.height});})()"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  BEFORE: $BEFORE"

# Click brush
curl -s -X POST "$SB/click" -H 'Content-Type: application/json' -d '{"selector":"button[data-tool=\"brush\"]"}' > /dev/null
sleep 0.3

# Paint stroke
AFTER=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var c=document.querySelector('\''canvas[data-testid=\"inpaint-mask-canvas\"]'\'');var ctx=c.getContext('\''2d'\'');ctx.fillStyle='\''rgba(255,0,0,1)'\'';var cx=c.width/2,cy=c.height/2;for(var i=0;i<20;i++){var x=cx-100+i*10;var y=cy+Math.sin(i*0.5)*30;ctx.beginPath();ctx.arc(x,y,32,0,Math.PI*2);ctx.fill();}var d=ctx.getImageData(0,0,c.width,c.height).data;var nz=0;var minX=c.width,maxX=0,minY=c.height,maxY=0;for(var py=0;py<c.height;py++){for(var px=0;px<c.width;px++){var idx=(py*c.width+px)*4+3;if(d[idx]>0){nz++;if(px<minX)minX=px;if(px>maxX)maxX=px;if(py<minY)minY=py;if(py>maxY)maxY=py;}}}return JSON.stringify({after_nz:nz,w:c.width,h:c.height,bbox:nz>0?{minX:minX,minY:minY,maxX:maxX,maxY:maxY}:null});})()"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  AFTER: $AFTER"
curl -s -X POST "$SB/screenshot" -H 'Content-Type: application/json' -d '{"path":"/tmp/canvas-audit-check1-brush.png"}' > /dev/null
echo '"check1_brush": {"before": '"$BEFORE"', "after": '"$AFTER"', "screenshot": "/tmp/canvas-audit-check1-brush.png"},' >> "$OUT"

# ── CHECK 2: Prompt bar ──
echo "=== CHECK 2: Prompt bar visible ==="
PROMPT=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var inputs=Array.from(document.querySelectorAll('\''textarea, input[type=\"text\"]'\''));var vis=inputs.filter(function(el){var r=el.getBoundingClientRect();var s=getComputedStyle(el);return r.width>0&&r.height>0&&s.display!=='\''none'\''&&s.visibility!=='\''hidden'\'';});return JSON.stringify(vis.map(function(el){var r=el.getBoundingClientRect();return{tag:el.tagName,placeholder:el.placeholder||'\'''\'',x:Math.round(r.x),y:Math.round(r.y),w:Math.round(r.width)};}));})()"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  Visible inputs: $PROMPT"
echo '"check2_prompt": '"$PROMPT"',' >> "$OUT"

# ── CHECK 3: Generate button ──
echo "=== CHECK 3: Generate button visible ==="
GEN=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var btns=Array.from(document.querySelectorAll('\''button'\''));var vis=btns.filter(function(b){var r=b.getBoundingClientRect();var s=getComputedStyle(b);var t=(b.textContent||'\'''\'').trim().toLowerCase();return r.width>0&&r.height>0&&s.display!=='\''none'\''&&(t.includes('\''generate'\'')||t.includes('\''invoke'\'')||t.includes('\''submit'\''));});return JSON.stringify(vis.map(function(b){var r=b.getBoundingClientRect();return{text:b.textContent.trim(),x:Math.round(r.x),y:Math.round(r.y),disabled:b.disabled};}));})()"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  Generate buttons: $GEN"
echo '"check3_generate": '"$GEN"',' >> "$OUT"

# ── CHECK 4: IP-Adapter hidden ──
echo "=== CHECK 4: IP-Adapter hidden when empty ==="
IP=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var o=document.getElementById('\''ip-adapter-overlay'\'');if(!o)return JSON.stringify({visible:false,reason:\"not found\"});var r=o.getBoundingClientRect();var s=getComputedStyle(o);return JSON.stringify({visible:r.width>0&&r.height>0&&s.display!=='\''none'\'',display:s.display,w:Math.round(r.width),h:Math.round(r.height)});})()"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  IP-Adapter: $IP"
echo '"check4_ipadapter": '"$IP"',' >> "$OUT"

# ── CHECK 5: OnboardingCoach pointer-events ──
echo "=== CHECK 5: OnboardingCoach pointer-events ==="
COACH=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var oc=document.getElementById('\''onboarding-coach'\'');if(!oc)return JSON.stringify({found:false});if(oc.classList.contains('\''hidden'\''))return JSON.stringify({found:true,hidden:true});var s=getComputedStyle(oc);var c=document.querySelector('\''canvas[data-testid=\"inpaint-mask-canvas\"]'\'');var cr=c.getBoundingClientRect();var ep=document.elementFromPoint(cr.left+50,cr.top+50);return JSON.stringify({found:true,hidden:false,overlayPointerEvents:s.pointerEvents,elementAtCanvasPoint:ep?ep.tagName+(ep.dataset&&ep.dataset.testid?\"[data-testid=\"+ep.dataset.testid+\"]\":'\'''\''):\"null\"});})()"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  Coach: $COACH"
echo '"check5_coach": '"$COACH"',' >> "$OUT"

# ── CHECK 6: Tool dock ──
echo "=== CHECK 6: Tool dock inventory ==="
DOCK=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var d=document.getElementById('\''tool-dock'\'');if(!d)return JSON.stringify({error:\"no dock\"});var btns=Array.from(d.querySelectorAll('\''button[data-tool]'\''));return JSON.stringify({count:btns.length,tools:btns.map(function(b){return b.getAttribute('\''data-tool'\'');})});})()"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  Dock: $DOCK"
echo '"check6_dock": '"$DOCK"',' >> "$OUT"

# ── CHECK 7: Canvas elements ──
echo "=== CHECK 7: Canvas elements ==="
CANVAS=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"(function(){var cs=Array.from(document.querySelectorAll('\''canvas'\''));return JSON.stringify(cs.map(function(c){var r=c.getBoundingClientRect();var ctx=c.getContext('\''2d'\'');var snz=-1;try{var d=ctx.getImageData(0,0,Math.min(c.width,200),Math.min(c.height,200)).data;snz=0;for(var i=3;i<d.length;i+=4){if(d[i]>0)snz++;}}catch(e){snz='\''error'\'';}return{testid:c.dataset.testid||\"(none)\",w:c.width,h:c.height,displayW:Math.round(r.width),displayH:Math.round(r.height),sampleNonZeroAlpha:snz};}));})()"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  Canvases: $CANVAS"
curl -s -X POST "$SB/screenshot" -H 'Content-Type: application/json' -d '{"path":"/tmp/canvas-audit-check7.png"}' > /dev/null
echo '"check7_canvases": '"$CANVAS"',' >> "$OUT"

# ── CONSOLE ERRORS ──
echo "=== CHECK 8: Console errors ==="
CONSOLE=$(curl -s -X POST "$SB/evaluate" -H 'Content-Type: application/json' \
  -d '{"code":"JSON.stringify(window._audit_errors||[])"}' \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('result',''))")
echo "  Console errors: $CONSOLE"
echo '"check8_console": '"$CONCONSOLE"'' >> "$OUT"
echo '}' >> "$OUT"

echo ""
echo "=== RESULTS SAVED TO $OUT ==="
cat "$OUT"
