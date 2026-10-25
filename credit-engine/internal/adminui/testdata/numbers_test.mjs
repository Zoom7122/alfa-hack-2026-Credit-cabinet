import assert from "node:assert/strict";
import {ExactNumber, parseJSON, stringifyJSON, integer, copyJSON} from "../web/numbers.js";

let passed = 0;
const check = (name, action) => { action(); passed++; };
check("int64 precision", () => assert.equal(stringifyJSON(parseJSON('{"x":9007199254740993,"y":9223372036854775807}')), '{"x":9007199254740993,"y":9223372036854775807}'));
check("decimal token preservation", () => assert.equal(stringifyJSON(parseJSON('[0,0e0,-0,1.0,1E+2,1e-100]')), '[0,0e0,-0,1.0,1E+2,1e-100]'));
check("unknown and false", () => { const value = parseJSON('{"x":false,"y":null,"z":0}'); assert.equal(value.x, false); assert.equal(value.y, null); assert.ok(value.z instanceof ExactNumber); });
check("exact totals", () => assert.equal(integer(new ExactNumber("9007199254740993")) + 1n, 9007199254740994n));
check("safe object keys", () => { const value = parseJSON('{"__proto__":{"x":1}}'); assert.ok(Object.hasOwn(value, "__proto__")); assert.equal(Object.getPrototypeOf(value), null); });
check("duplicate keys", () => assert.throws(() => parseJSON('{"x":0,"x":false}')));
check("escaped duplicate keys", () => assert.throws(() => parseJSON('{"x":0,"\\u0078":false}')));
check("nested duplicates", () => assert.throws(() => parseJSON('{"x":{"a":0,"a":1}}')));
check("strings", () => assert.equal(parseJSON('"строка\\n\\u003cscript\\u003e"'), "строка\n<script>"));
check("invalid documents", () => { for (const text of ["", "{} {}", "[1,]", "{\"x\":}", "01", "+1", "1.", ".1", "NaN", "Infinity", "tru", "{\"x\":1,}"]) assert.throws(() => parseJSON(text), text); });
check("bounded tokens", () => assert.throws(() => parseJSON("1".repeat(257))));
check("bounded nesting", () => assert.throws(() => parseJSON("[".repeat(100) + "0" + "]".repeat(100))));
check("cycle", () => { const value = {}; value.next = value; assert.throws(() => stringifyJSON(value)); });
check("nonfinite native numbers", () => assert.throws(() => stringifyJSON({x: Infinity})));
check("ordinary stringify guard", () => assert.throws(() => JSON.stringify(new ExactNumber("1"))));
check("independent copy", () => { const before = parseJSON('{"x":[9007199254740993,false,null]}'); const after = copyJSON(before); after.x[1] = true; assert.equal(before.x[1], false); assert.equal(String(after.x[0]), "9007199254740993"); });
check("pretty roundtrip", () => { const value = parseJSON('{"x":[1e0,{"y":false}],"z":null}'); assert.equal(stringifyJSON(parseJSON(stringifyJSON(value, true))), stringifyJSON(value)); });

export const result = {passed};
