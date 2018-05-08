export default function Percentage(numerator, denominator) {
  this.decimal = -1;
  if (denominator !== 0) {
    this.decimal = numerator / denominator;
  }
}

Percentage.prototype.get = function() {
  return this.decimal;
};

Percentage.prototype.prettyRate = function() {
  if (this.decimal < 0) {
    return "N/A";
  } else {
    return (100*this.decimal).toFixed(1) + "%";
  }
};
