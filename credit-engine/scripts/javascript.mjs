import {writeFile} from "node:fs/promises";
import {result as numbers} from "../internal/adminui/testdata/numbers_test.mjs";
import {result as model} from "../internal/adminui/testdata/model_edit_test.mjs";
const result={passed:numbers.passed+model.passed,numbers:numbers.passed,model:model.passed};
await writeFile(process.env.CREDIT_ENGINE_UI_OUTPUT+"/javascript.json",JSON.stringify(result,null,2));
console.log(JSON.stringify(result));
