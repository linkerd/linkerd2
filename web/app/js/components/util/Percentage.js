export default function Percentage(numerator, denominator) {
  this.decimal = -1;
  if (denominator !== 0) {
    this.decimal = numerator / denominator;
  }
}

Percentage.prototype.get = function() { // eslint-disable-line func-names
  return this.decimal;
};

Percentage.prototype.prettyRate = function() { // eslint-disable-line func-names
  if (this.decimal < 0) {
    return 'N/A';
  } else {
    return `${(100 * this.decimal).toFixed(1)}%`;
  }
};
