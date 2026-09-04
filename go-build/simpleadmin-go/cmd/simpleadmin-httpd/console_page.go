//go:build linux
// +build linux

package main

const nativeConsoleHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Terminal</title>
<style>
html,body{height:100%;margin:0;overflow:hidden;background:#050505;color:#f2f2f2;font-family:Consolas,Menlo,monospace}
body{display:flex;flex-direction:column;min-height:0}
#bar{flex:0 0 auto;min-height:40px;box-sizing:border-box;padding:9px 14px;background:#171717;border-bottom:1px solid #333;display:flex;gap:12px;align-items:center}
#state{font-size:14px;color:#9ad}
#hint{font-size:12px;color:#aaa}
#term{flex:1 1 auto;min-height:0;height:auto;box-sizing:border-box;overflow:auto;scrollbar-width:none;-ms-overflow-style:none;padding:12px;white-space:pre-wrap;word-break:break-word;outline:none;font-size:15px;line-height:1.35;background:#050505}
#term::-webkit-scrollbar{width:0;height:0}
.cursor::after{content:"_";animation:blink 1s steps(1) infinite}@keyframes blink{50%{opacity:0}}
#reconnect{display:none;margin-left:auto;padding:4px 14px;font-size:13px;font-family:inherit;color:#eee;background:#2b5d8a;border:1px solid #4a8ac2;border-radius:4px;cursor:pointer}
#reconnect:hover{background:#3a72a8}
#tip{font-size:12px;color:#e0b060}
</style>
</head>
<body>
<div id="bar"><strong>Terminal</strong><span id="state">connecting...</span><span id="hint">Click here and type. Ctrl+C / Ctrl+D / arrows are supported.</span><button id="reconnect" type="button">重新连接</button><span id="tip"></span></div>
<pre id="term" class="cursor" tabindex="0"></pre>
<script>
(function(){
  var term=document.getElementById('term');
  var state=document.getElementById('state');
  var tip=document.getElementById('tip');
  var reconnectBtn=document.getElementById('reconnect');
  var decoder=new TextDecoder('utf-8');
  var encoder=new TextEncoder();
  var scheme=location.protocol==='https:'?'wss':'ws';
  var ws=null;
  var resizeTimer=null;

  function setState(text){ state.textContent=text; }
  // 仅当用户停留在底部附近(距底 <40px)时才跟随输出自动滚动,
  // 向上翻看历史输出时不被新输出强行拉回底部。
  function scrollBottom(){
    if(term.scrollHeight-term.scrollTop-term.clientHeight<40) term.scrollTop=term.scrollHeight;
  }
  var ansiState={fg:'',bg:'',bold:false,dim:false,underline:false};
  var ansiCarry='';
  // 段尾孤立 \r 可能是 \r\n 的前半,延迟到下一段判定,避免误清当前行。
  var crPending=false;
  var ansiColors={
    30:'#000000',31:'#cc5555',32:'#55cc55',33:'#cccc55',34:'#5555cc',35:'#cc55cc',36:'#55cccc',37:'#dddddd',
    90:'#777777',91:'#ff7777',92:'#77ff77',93:'#ffff77',94:'#7777ff',95:'#ff77ff',96:'#77ffff',97:'#ffffff',
    40:'#000000',41:'#5f1f1f',42:'#1f5f1f',43:'#5f5f1f',44:'#1f1f5f',45:'#5f1f5f',46:'#1f5f5f',47:'#dddddd',
    100:'#555555',101:'#7f3333',102:'#337f33',103:'#7f7f33',104:'#33337f',105:'#7f337f',106:'#337f7f',107:'#ffffff'
  };
  var ansi256=[
    '#000000','#800000','#008000','#808000','#000080','#800080','#008080','#c0c0c0',
    '#808080','#ff0000','#00ff00','#ffff00','#0000ff','#ff00ff','#00ffff','#ffffff'
  ];
  function xterm256Color(n){
    n=Number(n);
    if(n>=0 && n<16) return ansi256[n];
    if(n>=16 && n<=231){
      var c=n-16;
      var r=Math.floor(c/36), g=Math.floor((c%36)/6), b=c%6;
      var conv=function(v){ return v===0?0:55+v*40; };
      return 'rgb('+conv(r)+','+conv(g)+','+conv(b)+')';
    }
    if(n>=232 && n<=255){
      var level=8+(n-232)*10;
      return 'rgb('+level+','+level+','+level+')';
    }
    return '';
  }
  function resetAnsi(){ ansiState={fg:'',bg:'',bold:false,dim:false,underline:false}; }
  function applySgr(params){
    if(!params.length) params=['0'];
    for(var i=0;i<params.length;i++){
      var raw=params[i];
      var code=raw===''?0:Number(raw);
      if(!isFinite(code)) continue;
      if(code===0){ resetAnsi(); }
      else if(code===1){ ansiState.bold=true; ansiState.dim=false; }
      else if(code===2){ ansiState.dim=true; }
      else if(code===4){ ansiState.underline=true; }
      else if(code===22){ ansiState.bold=false; ansiState.dim=false; }
      else if(code===24){ ansiState.underline=false; }
      else if(code===39){ ansiState.fg=''; }
      else if(code===49){ ansiState.bg=''; }
      else if((code>=30 && code<=37) || (code>=90 && code<=97)){ ansiState.fg=ansiColors[code] || ''; }
      else if((code>=40 && code<=47) || (code>=100 && code<=107)){ ansiState.bg=ansiColors[code] || ''; }
      else if((code===38 || code===48) && params[i+1]==='5' && i+2<params.length){
        var color=xterm256Color(params[i+2]);
        if(code===38) ansiState.fg=color; else ansiState.bg=color;
        i+=2;
      }else if((code===38 || code===48) && params[i+1]==='2' && i+4<params.length){
        var r=Number(params[i+2]), g=Number(params[i+3]), b=Number(params[i+4]);
        if(isFinite(r) && isFinite(g) && isFinite(b)){
          var rgb='rgb('+Math.max(0,Math.min(255,r))+','+Math.max(0,Math.min(255,g))+','+Math.max(0,Math.min(255,b))+')';
          if(code===38) ansiState.fg=rgb; else ansiState.bg=rgb;
        }
        i+=4;
      }
    }
  }
  function spanHasStyle(){ return ansiState.fg || ansiState.bg || ansiState.bold || ansiState.dim || ansiState.underline; }
  function appendStyledText(text){
    if(!text) return;
    var node;
    if(spanHasStyle()){
      node=document.createElement('span');
      if(ansiState.fg) node.style.color=ansiState.fg;
      if(ansiState.bg) node.style.backgroundColor=ansiState.bg;
      if(ansiState.bold) node.style.fontWeight='700';
      if(ansiState.dim) node.style.opacity='0.65';
      if(ansiState.underline) node.style.textDecoration='underline';
      node.textContent=text;
    }else{
      node=document.createTextNode(text);
    }
    term.appendChild(node);
  }
  function removeLastCharacter(){
    var node=term.lastChild;
    while(node){
      var text=node.textContent || '';
      if(text.length>0){
        node.textContent=text.slice(0,-1);
        if(!node.textContent && node.parentNode) node.parentNode.removeChild(node);
        return;
      }
      var prev=node.previousSibling;
      if(node.parentNode) node.parentNode.removeChild(node);
      node=prev;
    }
  }
  // 回车(\r)把光标移回当前行行首:近似为"丢弃当前行已渲染内容",
  // 使进度条/计数器等原行刷新输出覆盖当前行而不是无限追加。
  // 从末尾向前找到最近一个换行,截断其后内容。
  function removeCurrentLine(){
    var nodes=term.childNodes;
    for(var n=nodes.length-1;n>=0;n--){
      var node=nodes[n];
      var text=node.textContent || '';
      var idx=text.lastIndexOf('\n');
      if(idx>=0){
        node.textContent=text.slice(0,idx+1);
        while(term.lastChild && term.lastChild!==node){ term.removeChild(term.lastChild); }
        return;
      }
      if(node.parentNode) node.parentNode.removeChild(node);
    }
  }
  function clearTerm(){
    term.textContent='';
    resetAnsi();
  }
  function pruneTerm(){
    if(term.textContent.length<=200000) return;
    term.textContent=term.textContent.slice(-120000);
  }
  function appendPlain(s){
    var start=0;
    for(var i=0;i<s.length;i++){
      var ch=s[i];
      if(ch==='\b' || ch==='\x7f' || ch==='\f' || ch==='\r'){
        if(i>start) appendStyledText(s.slice(start,i));
        if(ch==='\f') clearTerm();
        else if(ch==='\r') removeCurrentLine();
        else removeLastCharacter();
        start=i+1;
      }
    }
    if(start<s.length) appendStyledText(s.slice(start));
  }
  function appendText(s){
    s=ansiCarry+s;
    ansiCarry='';
    // 上一段以孤立 \r 收尾时,需等本段首字符判定它是 \r\n(换行)还是
    // 单独的回车(原行刷新),不能在本段内直接处理。
    if(crPending){
      if(s.length===0){ return; }
      crPending=false;
      if(s.charAt(0)!=='\n'){ s='\r'+s; }
    }
    if(s.length>0 && s.charAt(s.length-1)==='\r'){
      crPending=true;
      s=s.slice(0,-1);
    }
    // \r\n 归一为换行;剩余孤立 \r 由 appendPlain 按回车处理。
    s=s.replace(/\r\n/g,'\n');
    var i=0;
    while(i<s.length){
      var esc=s.indexOf('\x1b',i);
      if(esc<0){ appendPlain(s.slice(i)); break; }
      if(esc>i) appendPlain(s.slice(i,esc));
      if(esc+1>=s.length){ ansiCarry=s.slice(esc); break; }
      var next=s[esc+1];
      if(next==='['){
        var end=esc+2;
        while(end<s.length && (s.charCodeAt(end)<0x40 || s.charCodeAt(end)>0x7e)) end++;
        if(end>=s.length){ ansiCarry=s.slice(esc); break; }
        var final=s[end];
        if(final==='m') applySgr(s.slice(esc+2,end).split(';'));
        i=end+1;
      }else if(next===']'){
        var bel=s.indexOf('\x07',esc+2);
        var st=s.indexOf('\x1b\\',esc+2);
        var oscEnd=-1;
        if(bel>=0 && st>=0) oscEnd=Math.min(bel,st+1);
        else if(bel>=0) oscEnd=bel;
        else if(st>=0) oscEnd=st+1;
        if(oscEnd<0){ ansiCarry=s.slice(esc); break; }
        i=oscEnd+1;
      }else if(next==='(' || next===')'){
        if(esc+2>=s.length){ ansiCarry=s.slice(esc); break; }
        i=esc+3;
      }else{
        i=esc+2;
      }
    }
    pruneTerm();
    scrollBottom();
  }
  function sendBytes(text){
    if(ws && ws.readyState===WebSocket.OPEN) ws.send(encoder.encode(text));
  }
  // 按终端容器实际尺寸估算行列数:字宽用等宽字体画布测量,
  // 行高取计算后的 line-height,扣除容器内边距(12px*2)。
  function terminalSize(){
    var style=getComputedStyle(term);
    var canvas=terminalSize.canvas || (terminalSize.canvas=document.createElement('canvas'));
    var ctx=canvas.getContext('2d');
    ctx.font=style.font;
    var cw=ctx.measureText('XXXXXXXXXX').width/10;
    var lh=parseFloat(style.lineHeight);
    if(!(cw>0)) cw=9;
    if(!(lh>0)) lh=20;
    var cols=Math.floor((term.clientWidth-24)/cw);
    var rows=Math.floor((term.clientHeight-24)/lh);
    if(cols<2) cols=2;
    if(rows<2) rows=2;
    if(cols>1000) cols=1000;
    if(rows>1000) rows=1000;
    return {cols:cols,rows:rows};
  }
  // resize 控制帧:文本帧承载 JSON,与二进制键入帧互不冲突;
  // 服务端解析失败时会静默忽略,不影响终端输入。
  function sendResize(){
    if(ws && ws.readyState===WebSocket.OPEN){
      var size=terminalSize();
      ws.send(JSON.stringify({type:'resize',cols:size.cols,rows:size.rows}));
    }
  }
  // (重新)建立终端连接并重走登录流程。所有回调绑定到本次 socket,
  // 旧连接迟到的 onclose 不会污染新连接的状态。
  function connect(){
    if(ws && (ws.readyState===WebSocket.CONNECTING || ws.readyState===WebSocket.OPEN)) return;
    reconnectBtn.style.display='none';
    tip.textContent='';
    setState('connecting...');
    var socket=new WebSocket(scheme+'://'+location.host+'/api/console/ws');
    socket.binaryType='arraybuffer';
    ws=socket;
    socket.onopen=function(){
      if(ws!==socket) return;
      setState('connected');
      term.focus();
      sendResize();
    };
    socket.onclose=function(){
      if(ws!==socket) return;
      setState('closed');
      appendText('\n[console closed]\n');
      tip.textContent='连接已断开。点击「重新连接」按钮可重新登录终端;若网络已恢复而按钮无效,请刷新页面。';
      reconnectBtn.style.display='';
    };
    socket.onerror=function(){
      if(ws!==socket) return;
      setState('error');
    };
    socket.onmessage=function(ev){
      if(ws!==socket) return;
      if(ev.data instanceof ArrayBuffer){ appendText(decoder.decode(ev.data,{stream:true})); }
      else { appendText(String(ev.data)); }
    };
  }
  reconnectBtn.addEventListener('click',function(){ connect(); });
  window.addEventListener('resize',function(){
    if(resizeTimer) clearTimeout(resizeTimer);
    resizeTimer=setTimeout(sendResize,200);
  });
  connect();

  document.addEventListener('keydown',function(e){
    if(document.activeElement!==term) term.focus();
    // Ctrl+Shift+C 交还给浏览器做复制,不当作 ^C 发送给终端。
    if(e.ctrlKey && e.shiftKey && (e.key==='c' || e.key==='C')) return;
    var v=null;
    if(e.ctrlKey){
      var k=e.key.toLowerCase();
      if(k==='c') v='\x03';
      else if(k==='d') v='\x04';
      else if(k==='l') v='\x0c';
      else if(k==='z') v='\x1a';
    }else if(e.key==='Enter') v='\r';
    else if(e.key==='Backspace') v='\x7f';
    else if(e.key==='Tab') v='\t';
    else if(e.key==='ArrowUp') v='\x1b[A';
    else if(e.key==='ArrowDown') v='\x1b[B';
    else if(e.key==='ArrowRight') v='\x1b[C';
    else if(e.key==='ArrowLeft') v='\x1b[D';
    else if(e.key==='Home') v='\x1b[H';
    else if(e.key==='End') v='\x1b[F';
    else if(e.key==='Delete') v='\x1b[3~';
    else if(e.key==='PageUp') v='\x1b[5~';
    else if(e.key==='PageDown') v='\x1b[6~';
    else if(e.key.length===1) v=e.key;
    if(v!==null){ e.preventDefault(); sendBytes(v); }
  });
  term.addEventListener('paste',function(e){
    e.preventDefault();
    var text=(e.clipboardData||window.clipboardData).getData('text') || '';
    sendBytes(text.replace(/\r?\n/g,'\r'));
  });
  term.addEventListener('click',function(){ term.focus(); });
})();
</script>
</body>
</html>
`
