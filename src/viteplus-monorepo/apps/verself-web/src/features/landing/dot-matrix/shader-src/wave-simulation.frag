precision highp float;

const int MAX_WAVE_IMPULSES = 6;

uniform float uActive;
uniform float uAmbient;
uniform float uDamping;
uniform float uDelta;
uniform float uWaveSpeed;
uniform int uImpulseCount;
uniform sampler2D uState;
uniform vec2 uTexel;
uniform vec4 uImpulseShapes[MAX_WAVE_IMPULSES];
uniform vec4 uImpulses[MAX_WAVE_IMPULSES];

in vec2 vUv;

out vec4 fragColor;

float impulseAt(vec2 uv) {
  float impulse = 0.0;

  for (int index = 0; index < MAX_WAVE_IMPULSES; index += 1) {
    if (index >= uImpulseCount) {
      break;
    }
    vec4 source = uImpulses[index];
    vec4 shape = uImpulseShapes[index];
    float radius = max(source.z, 0.0001);

    if (shape.w > 0.5) {
      vec2 normal = normalize(shape.xy);
      vec2 tangent = vec2(-normal.y, normal.x);
      vec2 offset = uv - source.xy;
      float acrossFront = dot(offset, tangent) / max(shape.z, 0.0001);
      float throughFront = dot(offset, normal) / radius;
      impulse += source.w * exp(-(acrossFront * acrossFront + throughFront * throughFront));
    } else {
      float distanceToSource = distance(uv, source.xy);
      impulse += source.w * exp(-(distanceToSource * distanceToSource) / (radius * radius));
    }
  }

  return impulse;
}

void main() {
  vec2 uv = vUv;
  vec4 centerState = texture(uState, uv);
  float height = centerState.r;
  float previous = centerState.g;

  float north = texture(uState, uv + vec2(0.0, uTexel.y)).r;
  float south = texture(uState, uv - vec2(0.0, uTexel.y)).r;
  float east = texture(uState, uv + vec2(uTexel.x, 0.0)).r;
  float west = texture(uState, uv - vec2(uTexel.x, 0.0)).r;
  float laplacian = north + south + east + west - 4.0 * height;

  float stepScale = clamp(uDelta * 60.0, 0.25, 1.25);
  float nextHeight =
    2.0 * height - previous + uWaveSpeed * laplacian * stepScale * stepScale;
  nextHeight += impulseAt(uv) * uActive;

  float damping = uDamping + uAmbient * 0.002;
  nextHeight *= pow(clamp(damping, 0.74, 0.995), stepScale);
  nextHeight = clamp(nextHeight, -1.0, 1.0);

  fragColor = vec4(nextHeight, height, abs(nextHeight - height), 1.0);
}
