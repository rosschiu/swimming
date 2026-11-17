import Ajv from 'ajv';
import { expect } from '@playwright/test';
import contract from '../contracts/openapi.json';

const ajv = new Ajv({ strict: false, allErrors: true });
const schema = { $id: 'swimming-contract', components: contract.components };
ajv.addSchema(schema);

export function expectContract(name: keyof typeof contract.components.schemas, data: unknown) {
  const validate = ajv.compile({ $ref: `swimming-contract#/components/schemas/${name}` });
  expect(validate(data), JSON.stringify(validate.errors)).toBe(true);
}
