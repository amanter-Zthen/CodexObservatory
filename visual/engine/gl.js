// Minimal WebGL2 helpers. No library: the native port maps these to GL 3.3 core.
(function (NS) {
  'use strict';
  const VS_FULL = `#version 300 es
  layout(location=0) in vec2 aP; out vec2 vUV;
  void main(){ vUV = aP*0.5+0.5; gl_Position = vec4(aP,0.,1.); }`;

  function compile(gl, type, src, name) {
    const s = gl.createShader(type); gl.shaderSource(s, src); gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
      const log = gl.getShaderInfoLog(s); console.error(name, log);
      throw new Error(name + ': ' + log);
    }
    return s;
  }
  function program(gl, vs, fs, name) {
    const p = gl.createProgram();
    gl.attachShader(p, compile(gl, gl.VERTEX_SHADER, vs, name + '.vs'));
    gl.attachShader(p, compile(gl, gl.FRAGMENT_SHADER, fs, name + '.fs'));
    gl.linkProgram(p);
    if (!gl.getProgramParameter(p, gl.LINK_STATUS)) throw new Error(name + ' link: ' + gl.getProgramInfoLog(p));
    const u = {}, n = gl.getProgramParameter(p, gl.ACTIVE_UNIFORMS);
    for (let i = 0; i < n; i++) { const info = gl.getActiveUniform(p, i); const key = info.name.replace(/\[0\]$/, ''); u[key] = gl.getUniformLocation(p, info.name); }
    return { p, u };
  }
  function texture(gl, w, h, fmt) {
    const t = gl.createTexture(); gl.bindTexture(gl.TEXTURE_2D, t);
    gl.texImage2D(gl.TEXTURE_2D, 0, fmt.internal, w, h, 0, fmt.format, fmt.type, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    return t;
  }
  // Render target with N colour attachments.
  function target(gl, w, h, fmt, n) {
    const fb = gl.createFramebuffer(); gl.bindFramebuffer(gl.FRAMEBUFFER, fb);
    const tex = [], bufs = [];
    for (let i = 0; i < n; i++) {
      tex.push(texture(gl, w, h, fmt));
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0 + i, gl.TEXTURE_2D, tex[i], 0);
      bufs.push(gl.COLOR_ATTACHMENT0 + i);
    }
    gl.drawBuffers(bufs);
    const ok = gl.checkFramebufferStatus(gl.FRAMEBUFFER) === gl.FRAMEBUFFER_COMPLETE;
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    return { fb, tex, w, h, ok, free() { gl.deleteFramebuffer(fb); tex.forEach((t) => gl.deleteTexture(t)); } };
  }
  function formats(gl) {
    const half = !!gl.getExtension('EXT_color_buffer_float') || !!gl.getExtension('EXT_color_buffer_half_float');
    gl.getExtension('OES_texture_float_linear');
    return half ? { internal: gl.RGBA16F, format: gl.RGBA, type: gl.HALF_FLOAT, hdr: true }
      : { internal: gl.RGBA8, format: gl.RGBA, type: gl.UNSIGNED_BYTE, hdr: false };
  }
  function quad(gl) {
    const vao = gl.createVertexArray(); gl.bindVertexArray(vao);
    const b = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, b);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, -1, 1, 1, -1, 1, 1]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0); gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
    gl.bindVertexArray(null);
    return vao;
  }
  // Instanced quad VAO: attribute 0 = corner, 1.. = per-instance vec4 slots.
  function instanced(gl, slots) {
    const vao = gl.createVertexArray(); gl.bindVertexArray(vao);
    const c = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, c);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([0, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 1]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0); gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
    const buf = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, buf);
    const stride = slots * 16;
    for (let i = 0; i < slots; i++) {
      gl.enableVertexAttribArray(1 + i); gl.vertexAttribPointer(1 + i, 4, gl.FLOAT, false, stride, i * 16); gl.vertexAttribDivisor(1 + i, 1);
    }
    gl.bindVertexArray(null);
    return { vao, buf, floats: slots * 4 };
  }
  NS.gl = { VS_FULL, program, texture, target, formats, quad, instanced };
})(window.RA);
