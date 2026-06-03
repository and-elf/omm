import { describe, expect, it } from 'vitest'

import { parseSetupLabel, SetupLabelError } from './setupLabel'

describe('parseSetupLabel', () => {
  it('parses OMM JSON', () => {
    expect(
      parseSetupLabel('{"ssid":"OMM-Setup-a51f","password":"secret","serial":"sn-1"}'),
    ).toEqual({ ssid: 'OMM-Setup-a51f', password: 'secret', serial: 'sn-1' })
  })

  it('parses JSON with only an ssid (open network)', () => {
    expect(parseSetupLabel('{"ssid":"OMM-Setup-a51f"}')).toEqual({
      ssid: 'OMM-Setup-a51f',
      password: undefined,
      serial: undefined,
    })
  })

  it('parses a WiFi URI', () => {
    expect(parseSetupLabel('WIFI:S:OMM-Setup-a51f;T:WPA;P:secret;;')).toEqual({
      ssid: 'OMM-Setup-a51f',
      password: 'secret',
      serial: undefined,
    })
  })

  it('parses a WiFi URI with a vendor serial field and escaped characters', () => {
    const creds = parseSetupLabel('WIFI:S:OMM\\;Setup;P:p\\:ss;SN:sn-9;;')
    expect(creds.ssid).toBe('OMM;Setup')
    expect(creds.password).toBe('p:ss')
    expect(creds.serial).toBe('sn-9')
  })

  it('treats a bare token as an open-network SSID', () => {
    expect(parseSetupLabel('OMM-Setup-a51f')).toEqual({ ssid: 'OMM-Setup-a51f' })
  })

  it('rejects empty input', () => {
    expect(() => parseSetupLabel('   ')).toThrow(SetupLabelError)
  })

  it('rejects JSON without an ssid', () => {
    expect(() => parseSetupLabel('{"password":"x"}')).toThrow(SetupLabelError)
  })

  it('rejects malformed JSON', () => {
    expect(() => parseSetupLabel('{not json')).toThrow(SetupLabelError)
  })

  it('rejects an unrecognised delimited format', () => {
    expect(() => parseSetupLabel('foo:bar;baz')).toThrow(SetupLabelError)
  })
})
