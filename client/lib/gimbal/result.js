export class GimbalResult {
  constructor(executor) {
    this._executor = executor;
    this._promise = null;
    this._value = undefined;
    this._settled = false;
    this._error = null;
  
  }

  _resolve() {
    if (!this._promise) {
      this._promise = Promise.resolve().then(async () => {
        try {
          this._value = await this._executor();
          this._settled = true;
          return this._value;
        } catch (e) {
          this._error = e;
          this._settled = true;
          throw e;
        }
      });
    }
    return this._promise;
  }

  then(onFulfilled, onRejected) {
    return this._resolve().then(onFulfilled, onRejected);
  }

  catch(onRejected) {
    return this._resolve().catch(onRejected);
  }

  toString() {
    if (!this._settled) return 'GR(pending)';
    if (this._error) return `GR(error: ${this._error.message ?? this._error})`;
    return `GR(${_tn(this._value)})`;
  }
}

function _tn(v) {
  if (v === null) return 'null';
  if (v === undefined) return 'undefined';
  return v?.constructor?.name ?? typeof v;
}
