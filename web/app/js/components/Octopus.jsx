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
import _ from 'lodash';
import { getSuccessRateClassification } from './util/MetricUtils.jsx' ;

const maxNumNeighbors = 6; // max number of neighbor nodes to show in the octopus graph

export default class Octopus extends React.Component {
  static defaultProps = {
    neighbors: {},
    resource: {},
    unmeshedSources: []
  }

  static propTypes = {
    neighbors: PropTypes.shape({}),
    resource: PropTypes.shape({}),
    unmeshedSources: PropTypes.arrayOf(PropTypes.shape({})),
  }

  getNeighborDisplayData = neighbors => {
    // only display maxNumNeighbors neighboring nodes in the octopus graph,
    // otherwise it will be really tall
    let upstreams = _.sortBy(neighbors.upstream, "resource.successRate");
    let downstreams = _.sortBy(neighbors.downstream, "resource.successRate");

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

    if (_.size(upstreams) > maxNumNeighbors) {
      display.upstreams.displayed = _.take(upstreams, maxNumNeighbors);
      display.upstreams.collapsed = _.slice(upstreams, maxNumNeighbors, _.size(upstreams));
    }

    if (_.size(downstreams) > maxNumNeighbors) {
      display.downstreams.displayed = _.take(downstreams, maxNumNeighbors);
      display.downstreams.collapsed = _.slice(downstreams, maxNumNeighbors, _.size(downstreams));
    }

    return display;
  }

  linkedResourceTitle = (resource, display) => {
    return _.isNil(resource.namespace) ? display :
    <this.props.api.ResourceLink
      resource={resource}
      linkText={display} />;
  }

  renderResourceCard(resource, type) {
    let display = displayName(resource);
    let classification = getSuccessRateClassification(resource.successRate);
    let Progress = StyledProgress(classification);

    return (
      <Grid item key={resource.name} >
        <Card className={`octopus-body-node ${type}`} title={display}>
          <CardContent>

            <Typography variant={type === "neighbor" ? "subtitle1" : "h6"} align="center">
              { this.linkedResourceTitle(resource, display) }
            </Typography>

            <Progress variant="determinate" value={resource.successRate * 100} />

            <Table padding="dense">
              <TableBody>
                <TableRow>
                  <TableCell><Typography>SR</Typography></TableCell>
                  <TableCell numeric={true}><Typography>{metricToFormatter["SUCCESS_RATE"](resource.successRate)}</Typography></TableCell>
                </TableRow>
                <TableRow>
                  <TableCell><Typography>RPS</Typography></TableCell>
                  <TableCell numeric={true}><Typography>{metricToFormatter["NO_UNIT"](resource.requestRate)}</Typography></TableCell>
                </TableRow>
                <TableRow>
                  <TableCell><Typography>P99</Typography></TableCell>
                  <TableCell numeric={true}><Typography>{metricToFormatter["LATENCY"](_.get(resource, "latency.P99"))}</Typography></TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </Grid>
    );
  }

  renderUnmeshedResources = unmeshedResources => {
    return (
      <Grid item>
        <Card key="unmeshed-resources" className="octopus-body-node neighbor">
          <CardContent>
            <Typography variant="subtitle1">Unmeshed</Typography>
            {
              _.map(unmeshedResources, r => {
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
    return (
      <Grid item>
        <Card className="octopus-body-node neighbor">
          <CardContent>
            {
              _.map(neighbors, r => {
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
    let middleElementIndex = isEven ? ((numNeighbors - 1) / 2) : _.floor(numNeighbors / 2);

    let arrowTypes = _.map(_.times(numNeighbors), i => {
      if (i < middleElementIndex) {
        let height = (_.ceil(middleElementIndex - i) - 1) * baseHeight + (baseHeight / 2);
        return { type: "up", inboundType: "down", height };
      } else if (i === middleElementIndex) {
        return { type: "flat", inboundType: "flat", height: baseHeight };
      } else {
        let height = (_.ceil(i - middleElementIndex) - 1) * baseHeight + (baseHeight / 2);
        return { type: "down", inboundType: "up", height };
      }
    });

    let height = numNeighbors * baseHeight;
    let svg = (
      <svg height={height} width={width} version="1.1" viewBox={`0 0 ${width} ${height}`}>
        <defs />
        {
          _.map(arrowTypes, arrow => {
            let arrowType = isOutbound ? arrow.type : arrow.inboundType;
            return OctopusArms[arrowType](width, height, arrow.height, isOutbound, isEven);
          })
        }
      </svg>
    );

    return !showArrow ? null : svg;
  }

  render() {
    let { resource, neighbors, unmeshedSources } = this.props;

    if (_.isEmpty(resource)) {
      return null;
    }

    let display = this.getNeighborDisplayData(neighbors);

    let numUpstreams = _.size(display.upstreams.displayed) + (_.isEmpty(unmeshedSources) ? 0 : 1) +
      (_.isEmpty(display.upstreams.collapsed) ? 0 : 1);

    let numDownstreams = _.size(display.downstreams.displayed) + (_.isEmpty(display.downstreams.collapsed) ? 0 : 1);


    return (
      <div className="octopus-graph-container">
        <div className="octopus-graph">
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
              {_.map(display.upstreams.displayed, n => this.renderResourceCard(n, "neighbor"))}
              {_.isEmpty(unmeshedSources) ? null : this.renderUnmeshedResources(unmeshedSources)}
              {_.isEmpty(display.upstreams.collapsed) ? null : this.renderCollapsedNeighbors(display.upstreams.collapsed)}
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
              {_.map(display.downstreams.displayed, n => this.renderResourceCard(n, "neighbor"))}
              {_.isEmpty(display.downstreams.collapsed) ? null : this.renderCollapsedNeighbors(display.downstreams.collapsed)}
            </Grid>
          </Grid>
        </div>
      </div>
    );
  }
}
