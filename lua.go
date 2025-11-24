package leakybucketgcra

// Copyright (c) 2017 Pavel Pravosud
// https://github.com/rwz/redis-gcra/blob/master/vendor/perform_gcra_ratelimit.lua
// allowNScriptSrc is the Lua source for the limiter.
var allowNScriptSrc = `
redis.replicate_commands()

local rate_limit_key = KEYS[1]
local burst = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local period = tonumber(ARGV[3])
local cost = tonumber(ARGV[4])

local emission_interval = period / rate
local increment = emission_interval * cost
local burst_offset = emission_interval * burst
local now = redis.call("TIME")

local jan_1_2017 = 1483228800
now = (now[1] - jan_1_2017) + (now[2] / 1000000)

local tat = redis.call("GET", rate_limit_key)

if not tat then
  tat = now
else
  tat = tonumber(tat)
end

-- Impossible request: cost larger than burst. Deny (no retry).
if cost > burst then
   return {0, 0, "-1", tostring(tat - now)}
end

local new_tat = math.max(tat, now) + increment

local allow_at = new_tat - burst_offset
local diff = now - allow_at

local allowed
local retry_after
local reset_after

local remaining = math.floor(diff / emission_interval + 0.5)

if remaining < 0 then
  allowed = 0
  remaining = 0
  reset_after = tat - now
  retry_after = diff * -1
  if retry_after > burst then
    retry_after = -1
  end
else
  allowed = cost
  reset_after = new_tat - now
  redis.call("SET", rate_limit_key, new_tat, "EX", math.ceil(reset_after))
  retry_after = -1
end

return {allowed, remaining, tostring(retry_after), tostring(reset_after)}
`

// allowNScript is kept for radix users who want the preloaded script.
// var allowNScript = radix.NewEvalScript(1, allowNScriptSrc)
