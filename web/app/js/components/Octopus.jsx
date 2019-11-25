import { OctopusArms, baseHeight } from './util/OctopusArms.jsx';
import { displayName, metricToFormatter } from './util/Utils.js';

import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import { StyledProgress } from './util/Progress.jsx';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import TableCell from '@material-ui/core/TableCell';
import TableRow from '@material-ui/core/TableRow';
import Typography from '@material-ui/core/Typography';
import _ceil from 'lodash/ceil';
import _floor from 'lodash/floor';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isNil from 'lodash/isNil';
import _size from 'lodash/size';
import _slice from 'lodash/slice';
import _sortBy from 'lodash/sortBy';
import _take from 'lodash/take';
import _times from 'lodash/times';
import { getSuccessRateClassification } from './util/MetricUtils.jsx';
import { withStyles } from "@material-ui/core/styles";
import { withTranslation } from 'react-i18next';

const maxNumNeighbors = 6; // max number of neighbor nodes to show in the octopus graph

const styles = () => ({
  graphContainer: {
    overflowX: "auto",
    padding: "16px 0"
  },
  graph: {
    maxWidth: "974px",
    minWidth: "974px",
    marginLeft: "auto",
    marginRight: "auto"
  },
  centerNode: {
    width: "244px"
  },
  neighborNode: {
    width: "220px"
  }
});
class Octopus extends React.Component {
  static defaultProps = {
    neighbors: {},
    resource: {},
    unmeshedSources: []
  }

  static propTypes = {
    classes: PropTypes.shape({}).isRequired,
    neighbors: PropTypes.shape({}),
    resource: PropTypes.shape({}),
    t: PropTypes.func.isRequired,
    unmeshedSources: PropTypes.arrayOf(PropTypes.shape({})),
  }

  getNeighborDisplayData = neighbors => {
    // only display maxNumNeighbors neighboring nodes in the octopus graph,
    // otherwise it will be really tall
    // even though _sortBy is a stable sort, the order that this data is returned by the API
    // can change, so the original order of items can change; this means we have to sort by
    // name and by SR to ensure an actual stable sort
    let upstreams = _sortBy(_sortBy(neighbors.upstream, displayName), n => n.successRate);
    let downstreams = _sortBy(_sortBy(neighbors.downstream, displayName), n => n.successRate);

    let display = {
      upstreams: {
        displayed: upstreams,
        collapsed: []
      },
      downstreams: {
        displayed: downstreams,
        collapsed: []
      }
    };

    if (_size(upstreams) > maxNumNeighbors) {
      display.upstreams.displayed = _take(upstreams, maxNumNeighbors);
      display.upstreams.collapsed = _slice(upstreams, maxNumNeighbors, _size(upstreams));
    }

    if (_size(downstreams) > maxNumNeighbors) {
      display.downstreams.displayed = _take(downstreams, maxNumNeighbors);
      display.downstreams.collapsed = _slice(downstreams, maxNumNeighbors, _size(downstreams));
    }

    return display;
  }

  linkedResourceTitle = (resource, display) => {
    // trafficsplit leaf resources cannot be linked
    return _isNil(resource.namespace) || resource.isLeafService ? display :
    <this.props.api.ResourceLink
      resource={resource}
      linkText={display} />;
  }

  renderResourceCard(resource, type) {
    const { classes } = this.props;
    let display = displayName(resource);
    let classification = getSuccessRateClassification(resource.successRate);
    let Progress = StyledProgress(classification);

    // if the resource only has TCP stats, display those instead
    let showTcp = false;
    // trafficsplit leaf with zero traffic should still show HTTP stats
    if (_isNil(resource.successRate) && _isNil(resource.requestRate) &&
      resource.type !== "service") {
      showTcp = true;
    }

    return (
      <Grid item key={resource.type + "-" + resource.name} >
        <Card className={type === "neighbor" ? classes.neighborNode : classes.centerNode} title={display}>
          <CardContent>

            <Typography variant={type === "neighbor" ? "subtitle1" : "h6"} align="center">
              { this.linkedResourceTitle(resource, display) }
            </Typography>

            <Progress variant="determinate" value={resource.successRate * 100} />

            <Table padding="dense">
              {showTcp ? this.renderTCPStats(resource) : this.renderHttpStats(resource)}
            </Table>
          </CardContent>
        </Card>
      </Grid>
    );
  }

  renderHttpStats(resource) {
    return (
      <TableBody>
        {resource.isLeafService &&
        <TableRow>
          <TableCell><Typography>{this.props.t("Weight")}</Typography></TableCell>
          <TableCell numeric={true}><Typography>{resource.tsStats.weight}</Typography></TableCell>
        </TableRow>
        }
        <TableRow>
          <TableCell><Typography>{this.props.t("SR")}</Typography></TableCell>
          <TableCell numeric={true}><Typography>{metricToFormatter["SUCCESS_RATE"](resource.successRate)}</Typography></TableCell>
        </TableRow>
        <TableRow>
          <TableCell><Typography>{this.props.t("RPS")}</Typography></TableCell>
          <TableCell numeric={true}><Typography>{metricToFormatter["NO_UNIT"](resource.requestRate)}</Typography></TableCell>
        </TableRow>
        {!resource.isApexService &&
        <TableRow>
          <TableCell><Typography>{this.props.t("P99")}</Typography></TableCell>
          <TableCell numeric={true}><Typography>{metricToFormatter["LATENCY"](_get(resource, "latency.P99"))}</Typography></TableCell>
        </TableRow>
        }
      </TableBody>
    );
  }

  renderTCPStats(resource) {
    let { tcp } = resource;
    return (
      <TableBody>
        <TableRow>
          <TableCell><Typography>{this.props.t("Conn")}</Typography></TableCell>
          <TableCell numeric={true}><Typography>{metricToFormatter["NO_UNIT"](tcp.openConnections)}</Typography></TableCell>
        </TableRow>
        <TableRow>
          <TableCell><Typography>{this.props.t("Read")}</Typography></TableCell>
          <TableCell numeric={true}><Typography>{metricToFormatter["BYTES"](tcp.readRate)}</Typography></TableCell>
        </TableRow>
        <TableRow>
          <TableCell><Typography>{this.props.t("Write")}</Typography></TableCell>
          <TableCell numeric={true}><Typography>{metricToFormatter["BYTES"](tcp.writeRate)}</Typography></TableCell>
        </TableRow>
      </TableBody>
    );
  }

  renderUnmeshedResources = unmeshedResources => {
    const { classes } = this.props;
    return (
      <Grid item>
        <Card key="unmeshed-resources" className={classes.neighborNode}>
          <CardContent>
            <Typography variant="subtitle1">Unmeshed</Typography>
            {
              unmeshedResources.map(r => {
                let display = displayName(r);
                return <Typography key={display} variant="body2" title={display}>{display}</Typography>;
              })
            }
          </CardContent>
        </Card>
      </Grid>
    );
  }

  renderCollapsedNeighbors = neighbors => {
    const { classes } = this.props;
    return (
      <Grid item>
        <Card className={classes.neighborNode}>
          <CardContent>
            {
              neighbors.map(r => {
                let display = displayName(r);
                return <Typography key={display}>{this.linkedResourceTitle(r, display)}</Typography>;
              })
            }
          </CardContent>
        </Card>
      </Grid>
    );
  }

  renderArrowCol = (numNeighbors, isOutbound) => {
    let width = 80;
    let showArrow = numNeighbors > 0;
    let isEven = numNeighbors % 2 === 0;
    let middleElementIndex = isEven ? ((numNeighbors - 1) / 2) : _floor(numNeighbors / 2);

    let arrowTypes = _times(numNeighbors, i => i).map(i => {
      if (i < middleElementIndex) {
        let height = (_ceil(middleElementIndex - i) - 1) * baseHeight + (baseHeight / 2);
        return { type: "up", inboundType: "down", height };
      } else if (i === middleElementIndex) {
        return { type: "flat", inboundType: "flat", height: baseHeight };
      } else {
        let height = (_ceil(i - middleElementIndex) - 1) * baseHeight + (baseHeight / 2);
        return { type: "down", inboundType: "up", height };
      }
    });

    let height = numNeighbors * baseHeight;
    let svg = (
      <svg height={height} width={width} version="1.1" viewBox={`0 0 ${width} ${height}`}>
        <defs />
        {
          arrowTypes.map(arrow => {
            let arrowType = isOutbound ? arrow.type : arrow.inboundType;
            return OctopusArms[arrowType](width, height, arrow.height, isOutbound, isEven);
          })
        }
      </svg>
    );

    return !showArrow ? null : svg;
  }

  render() {
    let { resource, neighbors, unmeshedSources, classes } = this.props;

    if (_isEmpty(resource)) {
      return null;
    }

    let display = this.getNeighborDisplayData(neighbors);

    let numUpstreams = _size(display.upstreams.displayed) + (_isEmpty(unmeshedSources) ? 0 : 1) +
      (_isEmpty(display.upstreams.collapsed) ? 0 : 1);

    let numDownstreams = _size(display.downstreams.displayed) + (_isEmpty(display.downstreams.collapsed) ? 0 : 1);


    return (
      <div className={classes.graphContainer}>
        <div className={classes.graph}>
          <Grid
            container
            direction="row"
            justify="center"
            alignItems="center">


            <Grid
              container
              spacing={24}
              direction="column"
              justify="center"
              alignItems="center"
              item
              xs={3}>
              {display.upstreams.displayed.map(n => this.renderResourceCard(n, "neighbor"))}
              {_isEmpty(unmeshedSources) ? null : this.renderUnmeshedResources(unmeshedSources)}
              {_isEmpty(display.upstreams.collapsed) ? null : this.renderCollapsedNeighbors(display.upstreams.collapsed)}
            </Grid>

            <Grid item xs={1}>
              {this.renderArrowCol(numUpstreams, false)}
            </Grid>


            <Grid item xs={3}>
              {this.renderResourceCard(resource, "main")}
            </Grid>

            <Grid item xs={1}>
              {this.renderArrowCol(numDownstreams, true)}
            </Grid>

            <Grid
              container
              spacing={24}
              direction="column"
              justify="center"
              alignItems="center"
              item
              xs={3}>
              {display.downstreams.displayed.map(n => this.renderResourceCard(n, "neighbor"))}
              {_isEmpty(display.downstreams.collapsed) ? null : this.renderCollapsedNeighbors(display.downstreams.collapsed)}
            </Grid>
          </Grid>
        </div>
      </div>
    );
  }
}

export default withTranslation()(withStyles(styles)(Octopus));
